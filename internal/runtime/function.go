package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vibelang/internal/ast"
)

type Callable interface {
	Name() string
	Call(ctx context.Context, interpreter *Interpreter, args []CallArgument) (any, error)
}

type ToolCallable interface {
	Callable
	ToolSpec() ToolSpec
}

type ToolSpec struct {
	Name       string
	Params     []ast.Param
	ReturnType ast.TypeRef
	Body       string
}

type CallArgument struct {
	Name  string
	Value any
}

type builtinFunction struct {
	name       string
	call       func(context.Context, *Interpreter, []any) (any, error)
	tool       *ToolSpec
	defaults   map[string]any
	promptSafe bool
}

func (b *builtinFunction) Name() string {
	return b.name
}

func (b *builtinFunction) Call(ctx context.Context, interpreter *Interpreter, args []CallArgument) (any, error) {
	positional, err := bindBuiltinCallArguments(b, args)
	if err != nil {
		return nil, err
	}
	return b.call(ctx, interpreter, positional)
}

func (b *builtinFunction) ToolSpec() ToolSpec {
	if b.tool == nil {
		return ToolSpec{Name: b.name}
	}
	return *b.tool
}

type AIFunction struct {
	Def      *ast.FunctionDef
	defaults map[string]any
}

func NewAIFunction(def *ast.FunctionDef, defaults map[string]any) *AIFunction {
	copiedDefaults := make(map[string]any, len(defaults))
	for name, value := range defaults {
		copiedDefaults[name] = value
	}
	return &AIFunction{Def: def, defaults: copiedDefaults}
}

func (f *AIFunction) Name() string {
	return f.Def.Name
}

func (f *AIFunction) ToolSpec() ToolSpec {
	return ToolSpec{
		Name:       f.Def.Name,
		Params:     f.Def.Params,
		ReturnType: f.Def.ReturnType,
		Body:       f.Def.Body,
	}
}

func (f *AIFunction) Call(ctx context.Context, interpreter *Interpreter, args []CallArgument) (any, error) {
	bound, err := f.bindArguments(args)
	if err != nil {
		return nil, err
	}
	return interpreter.invokeAIFunction(ctx, f, bound, 0)
}

func (f *AIFunction) bindArguments(args []CallArgument) (map[string]any, error) {
	return bindCallArguments(f.Def.Name, f.Def.Params, args, f.defaults)
}

func (f *AIFunction) bindNamed(args map[string]any) (map[string]any, error) {
	callArgs := make([]CallArgument, 0, len(args))
	for name, value := range args {
		callArgs = append(callArgs, CallArgument{Name: name, Value: value})
	}
	return bindCallArguments(f.Def.Name, f.Def.Params, callArgs, f.defaults)
}

func (f *AIFunction) hasParam(name string) bool {
	for _, param := range f.Def.Params {
		if param.Name == name {
			return true
		}
	}
	return false
}

func bindBuiltinCallArguments(function *builtinFunction, args []CallArgument) ([]any, error) {
	if len(args) == 0 {
		return nil, nil
	}

	hasNamed := false
	positional := make([]any, 0, len(args))
	for _, arg := range args {
		if arg.Name != "" {
			hasNamed = true
			break
		}
		positional = append(positional, arg.Value)
	}
	if !hasNamed {
		return positional, nil
	}

	if function.tool == nil || len(function.tool.Params) == 0 {
		return nil, fmt.Errorf("%s does not accept keyword arguments", function.name)
	}

	bound, err := bindCallArguments(function.name, function.tool.Params, args, function.defaults)
	if err != nil {
		return nil, err
	}
	return positionalArguments(function.tool.Params, bound), nil
}

func bindCallArguments(functionName string, params []ast.Param, args []CallArgument, defaults map[string]any) (map[string]any, error) {
	bound := make(map[string]any, len(params))

	positionalIndex := 0
	for _, arg := range args {
		if arg.Name == "" {
			if positionalIndex >= len(params) {
				return nil, fmt.Errorf("%s expected at most %d arguments, got %d", functionName, len(params), len(args))
			}
			param := params[positionalIndex]
			if _, exists := bound[param.Name]; exists {
				return nil, fmt.Errorf("%s received multiple values for argument %q", functionName, param.Name)
			}
			coerced, err := Coerce(param.Type.String(), arg.Value)
			if err != nil {
				return nil, fmt.Errorf("argument %q: %w", param.Name, err)
			}
			bound[param.Name] = coerced
			positionalIndex++
			continue
		}

		if !hasParam(params, arg.Name) {
			return nil, fmt.Errorf("%s received unknown argument %q", functionName, arg.Name)
		}
		if _, exists := bound[arg.Name]; exists {
			return nil, fmt.Errorf("%s received multiple values for argument %q", functionName, arg.Name)
		}
		param := findParam(params, arg.Name)
		coerced, err := Coerce(param.Type.String(), arg.Value)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", arg.Name, err)
		}
		bound[arg.Name] = coerced
	}

	for _, param := range params {
		if _, ok := bound[param.Name]; ok {
			continue
		}
		if defaults != nil {
			if value, ok := defaults[param.Name]; ok {
				bound[param.Name] = value
				continue
			}
		}
		return nil, fmt.Errorf("%s missing argument %q", functionName, param.Name)
	}

	return bound, nil
}

func hasParam(params []ast.Param, name string) bool {
	for _, param := range params {
		if param.Name == name {
			return true
		}
	}
	return false
}

func findParam(params []ast.Param, name string) ast.Param {
	for _, param := range params {
		if param.Name == name {
			return param
		}
	}
	return ast.Param{}
}

func positionalArguments(params []ast.Param, bound map[string]any) []any {
	args := make([]any, 0, len(params))
	for _, param := range params {
		args = append(args, bound[param.Name])
	}
	return args
}

type aiAction struct {
	Action string
	Value  any
	Call   *toolInvocation
}

type toolInvocation struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func decodeAIAction(text string) (aiAction, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return aiAction{}, fmt.Errorf("empty model response")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		start := strings.IndexByte(raw, '{')
		end := strings.LastIndexByte(raw, '}')
		if start >= 0 && end > start {
			if secondErr := json.Unmarshal([]byte(raw[start:end+1]), &payload); secondErr == nil {
				return decodeActionPayload(payload)
			}
		}
		return aiAction{}, fmt.Errorf("response was not valid JSON: %w", err)
	}

	return decodeActionPayload(payload)
}

func decodeActionPayload(payload map[string]any) (aiAction, error) {
	if action, ok := payload["action"].(string); ok {
		switch action {
		case "return":
			value, ok := payload["value"]
			if !ok {
				value = payload["result"]
			}
			return aiAction{Action: "return", Value: value}, nil
		case "call":
			call, err := decodeToolInvocation(payload["call"])
			if err != nil {
				return aiAction{}, err
			}
			return aiAction{Action: "call", Call: call}, nil
		}
	}

	if _, ok := payload["value"]; ok {
		return aiAction{Action: "return", Value: payload["value"]}, nil
	}
	if _, ok := payload["result"]; ok {
		return aiAction{Action: "return", Value: payload["result"]}, nil
	}
	if callPayload, ok := payload["tool_call"]; ok {
		call, err := decodeToolInvocation(callPayload)
		if err != nil {
			return aiAction{}, err
		}
		return aiAction{Action: "call", Call: call}, nil
	}
	if functionName, ok := payload["function"].(string); ok {
		args, _ := payload["arguments"].(map[string]any)
		return aiAction{
			Action: "call",
			Call: &toolInvocation{
				Name:      functionName,
				Arguments: args,
			},
		}, nil
	}

	return aiAction{}, fmt.Errorf("response did not include a valid action")
}

func decodeToolInvocation(value any) (*toolInvocation, error) {
	callMap, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool call was not an object")
	}
	name, ok := callMap["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("tool call was missing a function name")
	}
	arguments, _ := callMap["arguments"].(map[string]any)
	if arguments == nil {
		arguments = map[string]any{}
	}
	return &toolInvocation{Name: name, Arguments: arguments}, nil
}
