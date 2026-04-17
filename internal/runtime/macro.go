package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vibelang/internal/ast"
	"vibelang/internal/model"
	"vibelang/internal/parser"
)

type MacroCallable interface {
	Name() string
	Expand(ctx context.Context, interpreter *Interpreter, env *Environment, args []CallArgument) (any, error)
}

type AIMacro struct {
	Def          *ast.MacroDef
	defaults     map[string]any
	captured     map[string]any
	instructions string
	directives   aiDirectiveConfig
}

func NewAIMacro(def *ast.MacroDef, defaults map[string]any, captured map[string]any) (*AIMacro, error) {
	copiedDefaults := make(map[string]any, len(defaults))
	for name, value := range defaults {
		copiedDefaults[name] = cloneValue(value)
	}
	copiedCaptured := make(map[string]any, len(captured))
	for name, value := range captured {
		copiedCaptured[name] = cloneValue(value)
	}
	directives, instructions, err := parseAIBody(def.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.Name, err)
	}
	return &AIMacro{
		Def:          def,
		defaults:     copiedDefaults,
		captured:     copiedCaptured,
		instructions: instructions,
		directives:   directives,
	}, nil
}

func (m *AIMacro) Name() string {
	return m.Def.Name
}

func (m *AIMacro) Expand(ctx context.Context, interpreter *Interpreter, env *Environment, args []CallArgument) (any, error) {
	bound, err := m.bindArguments(args)
	if err != nil {
		return nil, err
	}
	return interpreter.expandMacro(ctx, env, m, bound)
}

func (m *AIMacro) bindArguments(args []CallArgument) (map[string]any, error) {
	return bindCallArguments(m.Def.Name, m.Def.Params, args, m.defaults)
}

func (m *AIMacro) scope(args map[string]any) map[string]any {
	scope := make(map[string]any, len(m.captured)+len(args))
	for name, value := range m.captured {
		scope[name] = value
	}
	for name, value := range args {
		scope[name] = value
	}
	return scope
}

func (i *Interpreter) expandMacro(ctx context.Context, env *Environment, macro *AIMacro, args map[string]any) (any, error) {
	if i.model == nil {
		return nil, fmt.Errorf("no model client configured")
	}

	scope := macro.scope(args)
	instructions, err := i.renderPromptText(ctx, macro.instructions, scope)
	if err != nil {
		return nil, err
	}
	cacheFunction := &AIFunction{
		Def: &ast.FunctionDef{
			Name:       macro.Name(),
			ReturnType: macro.Def.ReturnType,
		},
	}
	cacheKey, cacheHit := i.maybeLookupAICache(cacheFunction, instructions, scope, macro.directives)
	if cacheHit {
		return cacheKey, nil
	}

	history := make([]ToolEvent, 0)
	tools := i.toolSpecs("", macro.directives)
	modelTools, err := buildModelToolDefinitions(tools)
	if err != nil {
		return nil, err
	}
	actionSchema, err := buildMacroActionSchema(tools)
	if err != nil {
		return nil, err
	}

	maxSteps := i.maxAISteps
	if macro.directives.MaxSteps != nil {
		maxSteps = *macro.directives.MaxSteps
	}

	for step := 0; step < maxSteps; step++ {
		prompt, err := buildMacroPrompt(macro, instructions, scope, tools, history, actionSchema)
		if err != nil {
			return nil, err
		}

		response, err := i.model.Generate(ctx, model.Request{
			System:      macroSystemPrompt(),
			Prompt:      prompt,
			JSONSchema:  actionSchema,
			Tools:       modelTools,
			Temperature: macro.directives.Temperature,
			MaxTokens:   macro.directives.MaxTokens,
		})
		i.incrementMetric("ai_model_requests_total", 1)
		if err != nil {
			i.incrementMetric("ai_model_request_errors_total", 1)
			return nil, fmt.Errorf("%s model request failed: %w", macro.Name(), err)
		}
		var action macroAction
		if response.ToolCall != nil {
			i.tracef("%s native macro tool call: %s with %s", macro.Name(), response.ToolCall.Name, jsonString(response.ToolCall.Arguments))
			action = macroAction{
				Action: "call",
				Call: &toolInvocation{
					Name:      response.ToolCall.Name,
					Arguments: response.ToolCall.Arguments,
				},
			}
		} else {
			i.tracef("%s raw macro response: %s", macro.Name(), response.Text)

			action, err = decodeAIMacroAction(response.Text)
			if err != nil {
				return nil, fmt.Errorf("%s returned invalid macro action: %w", macro.Name(), err)
			}
		}

		switch action.Action {
		case "expand":
			expr, err := parser.ParseExpressionSource(action.Source)
			if err != nil {
				return nil, fmt.Errorf("%s produced invalid expansion %q: %w", macro.Name(), action.Source, err)
			}
			value, err := i.evaluateExpression(ctx, env, expr)
			if err != nil {
				return nil, fmt.Errorf("%s expansion failed: %w", macro.Name(), err)
			}
			coerced, err := Coerce(macro.Def.ReturnType.String(), value)
			if err != nil {
				return nil, fmt.Errorf("%s expansion did not match %s: %w", macro.Name(), macro.Def.ReturnType.String(), err)
			}
			i.maybeStoreAICache(cacheKey, macro.directives, coerced)
			i.incrementMetric("ai_macro_expansions_total", 1)
			return coerced, nil
		case "call":
			if action.Call == nil {
				return nil, fmt.Errorf("%s requested a helper call without call details", macro.Name())
			}
			callee, ok := i.lookupTool(action.Call.Name)
			if !ok {
				return nil, fmt.Errorf("%s requested unknown helper %q", macro.Name(), action.Call.Name)
			}
			spec := callee.ToolSpec()
			callArgs := namedCallArguments(action.Call.Arguments)
			var bound map[string]any
			switch named := callee.(type) {
			case *AIFunction:
				bound, err = named.bindArguments(callArgs)
			case *builtinFunction:
				bound, err = bindCallArguments(action.Call.Name, spec.Params, callArgs, named.defaults)
			default:
				bound, err = bindCallArguments(action.Call.Name, spec.Params, callArgs, nil)
			}
			if err != nil {
				return nil, err
			}
			if !macro.directives.allowsTool(action.Call.Name) {
				rejection := fmt.Sprintf("the helper %s is not enabled for this macro", action.Call.Name)
				if repeatedRejectedToolCall(history, action.Call.Name, bound) {
					i.incrementMetric("ai_tool_call_retries_blocked_total", 1)
					return nil, fmt.Errorf("%s repeatedly requested the rejected helper %s(%s): %s", macro.Name(), action.Call.Name, jsonString(bound), rejection)
				}
				i.incrementMetric("ai_tool_call_rejections_total", 1)
				history = append(history, ToolEvent{
					Name:      action.Call.Name,
					Arguments: bound,
					Error:     rejection,
					Rejected:  true,
				})
				continue
			}
			i.incrementMetric("ai_tool_calls_total", 1)
			i.tracef("%s calling %s with %s", macro.Name(), action.Call.Name, jsonString(bound))
			result, err := i.invokeTool(ctx, callee, bound, 1, nil)
			if err != nil {
				i.incrementMetric("ai_tool_call_errors_total", 1)
				return nil, err
			}
			history = append(history, ToolEvent{
				Name:      action.Call.Name,
				Arguments: bound,
				Result:    result,
			})
		default:
			return nil, fmt.Errorf("%s returned unsupported action %q", macro.Name(), action.Action)
		}
	}

	return nil, fmt.Errorf("%s exceeded the maximum AI tool steps of %d", macro.Name(), maxSteps)
}

type macroAction struct {
	Action string
	Source string
	Call   *toolInvocation
}

func decodeAIMacroAction(text string) (macroAction, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return macroAction{}, fmt.Errorf("empty model response")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		start := strings.IndexByte(raw, '{')
		end := strings.LastIndexByte(raw, '}')
		if start >= 0 && end > start {
			if secondErr := json.Unmarshal([]byte(raw[start:end+1]), &payload); secondErr == nil {
				return decodeMacroActionPayload(payload)
			}
		}
		return macroAction{}, fmt.Errorf("response was not valid JSON: %w", err)
	}

	return decodeMacroActionPayload(payload)
}

func decodeMacroActionPayload(payload map[string]any) (macroAction, error) {
	if action, ok := payload["action"].(string); ok {
		switch action {
		case "expand":
			source, ok := payload["source"].(string)
			if !ok || strings.TrimSpace(source) == "" {
				return macroAction{}, fmt.Errorf("macro expansion was missing source")
			}
			return macroAction{Action: "expand", Source: strings.TrimSpace(source)}, nil
		case "call":
			call, err := decodeToolInvocation(payload["call"])
			if err != nil {
				return macroAction{}, err
			}
			return macroAction{Action: "call", Call: call}, nil
		}
	}

	if source, ok := payload["source"].(string); ok && strings.TrimSpace(source) != "" {
		return macroAction{Action: "expand", Source: strings.TrimSpace(source)}, nil
	}
	if callPayload, ok := payload["tool_call"]; ok {
		call, err := decodeToolInvocation(callPayload)
		if err != nil {
			return macroAction{}, err
		}
		return macroAction{Action: "call", Call: call}, nil
	}

	return macroAction{}, fmt.Errorf("response did not include a valid macro action")
}
