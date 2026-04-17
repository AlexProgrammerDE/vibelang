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
	modelClient, err := i.modelClientForDirectives(macro.directives)
	if err != nil {
		return nil, fmt.Errorf("%s model routing failed: %w", macro.Name(), err)
	}

	history := make([]ToolEvent, 0)
	tools := i.toolSpecs("", macro.directives)
	if !shouldExposeImplicitTools(instructions, tools, macro.directives) {
		tools = nil
	}
	modelTools, err := buildModelToolDefinitions(tools)
	if err != nil {
		return nil, err
	}
	actionSchema, err := buildMacroActionSchema(tools)
	if err != nil {
		return nil, err
	}
	expandOnlySchema, err := buildMacroActionSchema(nil)
	if err != nil {
		return nil, err
	}

	maxSteps := i.maxAISteps
	if macro.directives.MaxSteps != nil {
		maxSteps = *macro.directives.MaxSteps
	}

	for step := 0; step < maxSteps; step++ {
		promptTools := tools
		requestTools := modelTools
		requestSchema := actionSchema
		if shouldForceDirectAnswerAfterUnknownHelper(history) {
			promptTools = nil
			requestTools = nil
			requestSchema = expandOnlySchema
		}

		prompt, err := buildMacroPrompt(macro, instructions, scope, promptTools, history, requestSchema)
		if err != nil {
			return nil, err
		}

		response, err := modelClient.Generate(ctx, model.Request{
			System:      composeSystemPrompt(macroSystemPrompt(), macro.directives),
			Prompt:      prompt,
			JSONSchema:  requestSchema,
			Tools:       requestTools,
			Temperature: macro.directives.Temperature,
			MaxTokens:   macro.directives.MaxTokens,
		})
		i.incrementMetric("ai_model_requests_total", 1)
		if err != nil {
			i.incrementMetric("ai_model_request_errors_total", 1)
			return nil, fmt.Errorf("%s model request failed: %w", macro.Name(), err)
		}
		if len(response.ToolCalls) > 0 {
			calls := make([]toolInvocation, 0, len(response.ToolCalls))
			for _, toolCall := range response.ToolCalls {
				i.tracef("%s native macro tool call: %s with %s", macro.Name(), toolCall.Name, jsonString(toolCall.Arguments))
				calls = append(calls, toolInvocation{
					Name:      toolCall.Name,
					Arguments: toolCall.Arguments,
				})
			}
			history, err = i.executeMacroToolInvocations(ctx, macro, calls, history)
			if err != nil {
				return nil, err
			}
			continue
		}

		var action macroAction
		{
			i.tracef("%s raw macro response: %s", macro.Name(), response.Text)

			action, err = decodeAIMacroAction(response.Text)
			if err != nil {
				action, err = i.repairMacroAction(ctx, modelClient, macro, instructions, scope, response.Text, expandOnlySchema)
				if err != nil {
					return nil, fmt.Errorf("%s returned invalid macro action: %w", macro.Name(), err)
				}
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
			history, err = i.executeMacroToolInvocation(ctx, macro, *action.Call, history)
			if err != nil {
				return nil, err
			}
		case "call_many":
			if len(action.Calls) == 0 {
				return nil, fmt.Errorf("%s requested batched helper calls without call details", macro.Name())
			}
			history, err = i.executeMacroToolInvocations(ctx, macro, action.Calls, history)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%s returned unsupported action %q", macro.Name(), action.Action)
		}
	}

	return nil, fmt.Errorf("%s exceeded the maximum AI tool steps of %d", macro.Name(), maxSteps)
}

func (i *Interpreter) repairMacroAction(ctx context.Context, modelClient model.Client, macro *AIMacro, instructions string, scope map[string]any, invalidResponse string, schema map[string]any) (macroAction, error) {
	prompt, err := buildMacroRepairPrompt(macro, instructions, scope, invalidResponse, schema)
	if err != nil {
		return macroAction{}, err
	}

	response, err := modelClient.Generate(ctx, model.Request{
		System:      composeSystemPrompt(macroSystemPrompt(), macro.directives),
		Prompt:      prompt,
		JSONSchema:  schema,
		Temperature: macro.directives.Temperature,
		MaxTokens:   macro.directives.MaxTokens,
	})
	i.incrementMetric("ai_model_requests_total", 1)
	if err != nil {
		i.incrementMetric("ai_model_request_errors_total", 1)
		return macroAction{}, fmt.Errorf("%s macro repair request failed: %w", macro.Name(), err)
	}
	if len(response.ToolCalls) > 0 {
		return macroAction{}, fmt.Errorf("%s macro repair returned unexpected native tool calls", macro.Name())
	}
	i.tracef("%s repaired macro response: %s", macro.Name(), response.Text)

	action, err := decodeAIMacroAction(response.Text)
	if err != nil {
		return macroAction{}, err
	}
	return action, nil
}

type macroAction struct {
	Action string
	Source string
	Call   *toolInvocation
	Calls  []toolInvocation
}

func (i *Interpreter) executeMacroToolInvocation(ctx context.Context, macro *AIMacro, call toolInvocation, history []ToolEvent) ([]ToolEvent, error) {
	callee, ok := i.lookupTool(call.Name)
	if !ok {
		arguments := call.Arguments
		if arguments == nil {
			arguments = map[string]any{}
		}
		rejection := fmt.Sprintf("the helper %s is not available; choose one of the listed helpers or return a value", call.Name)
		i.incrementMetric("ai_tool_call_rejections_total", 1)
		i.tracef("%s rejected unknown helper %s with %s: %s", macro.Name(), call.Name, jsonString(arguments), rejection)
		history = append(history, ToolEvent{
			Name:      call.Name,
			Arguments: arguments,
			Error:     rejection,
			Rejected:  true,
		})
		return history, nil
	}
	spec := callee.ToolSpec()
	callArgs := namedCallArguments(call.Arguments)

	var (
		bound map[string]any
		err   error
	)
	switch named := callee.(type) {
	case *AIFunction:
		bound, err = named.bindArguments(callArgs)
	case *builtinFunction:
		bound, err = bindCallArguments(call.Name, spec.Params, callArgs, named.defaults)
	default:
		bound, err = bindCallArguments(call.Name, spec.Params, callArgs, nil)
	}
	if err != nil {
		return nil, err
	}

	if !macro.directives.allowsTool(call.Name) {
		rejection := fmt.Sprintf("the helper %s is not enabled for this macro", call.Name)
		if repeatedRejectedToolCall(history, call.Name, bound) {
			i.incrementMetric("ai_tool_call_retries_blocked_total", 1)
			return nil, fmt.Errorf("%s repeatedly requested the rejected helper %s(%s): %s", macro.Name(), call.Name, jsonString(bound), rejection)
		}
		i.incrementMetric("ai_tool_call_rejections_total", 1)
		history = append(history, ToolEvent{
			Name:      call.Name,
			Arguments: bound,
			Error:     rejection,
			Rejected:  true,
		})
		return history, nil
	}

	i.incrementMetric("ai_tool_calls_total", 1)
	i.tracef("%s calling %s with %s", macro.Name(), call.Name, jsonString(bound))
	result, err := i.invokeTool(ctx, callee, bound, 1, nil)
	if err != nil {
		i.incrementMetric("ai_tool_call_errors_total", 1)
		return nil, err
	}
	history = append(history, ToolEvent{
		Name:      call.Name,
		Arguments: bound,
		Result:    result,
	})
	return history, nil
}

func (i *Interpreter) executeMacroToolInvocations(ctx context.Context, macro *AIMacro, calls []toolInvocation, history []ToolEvent) ([]ToolEvent, error) {
	for _, call := range calls {
		var err error
		history, err = i.executeMacroToolInvocation(ctx, macro, call, history)
		if err != nil {
			return nil, err
		}
	}
	return history, nil
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
			call, err := decodeActionToolInvocation(payload)
			if err != nil {
				return macroAction{}, err
			}
			return macroAction{Action: "call", Call: call}, nil
		case "call_many":
			calls, err := decodeActionToolInvocations(payload)
			if err != nil {
				return macroAction{}, err
			}
			return macroAction{Action: "call_many", Calls: calls}, nil
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
	if callsPayload, ok := payload["calls"]; ok {
		calls, err := decodeToolInvocations(callsPayload)
		if err != nil {
			return macroAction{}, err
		}
		return macroAction{Action: "call_many", Calls: calls}, nil
	}
	if toolCallsPayload, ok := payload["tool_calls"]; ok {
		calls, err := decodeToolInvocations(toolCallsPayload)
		if err != nil {
			return macroAction{}, err
		}
		return macroAction{Action: "call_many", Calls: calls}, nil
	}
	if hasToolInvocationPayload(payload) {
		call, err := decodeToolInvocation(payload)
		if err != nil {
			return macroAction{}, err
		}
		return macroAction{Action: "call", Call: call}, nil
	}

	return macroAction{}, fmt.Errorf("response did not include a valid macro action")
}
