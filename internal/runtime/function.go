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
	bindArgs   bool
	hiddenTool bool
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
	Def          *ast.FunctionDef
	defaults     map[string]any
	captured     map[string]any
	instructions string
	directives   aiDirectiveConfig
}

func NewAIFunction(def *ast.FunctionDef, defaults map[string]any, captured map[string]any) (*AIFunction, error) {
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
	return &AIFunction{
		Def:          def,
		defaults:     copiedDefaults,
		captured:     copiedCaptured,
		instructions: instructions,
		directives:   directives,
	}, nil
}

func (f *AIFunction) Name() string {
	return f.Def.Name
}

func (f *AIFunction) ToolSpec() ToolSpec {
	return ToolSpec{
		Name:       f.Def.Name,
		Params:     f.Def.Params,
		ReturnType: f.Def.ReturnType,
		Body:       f.instructions,
	}
}

func (f *AIFunction) Call(ctx context.Context, interpreter *Interpreter, args []CallArgument) (any, error) {
	bound, err := f.bindArguments(args)
	if err != nil {
		return nil, err
	}
	return interpreter.invokeAIFunction(ctx, f, bound, 0, nil)
}

func (f *AIFunction) scope(args map[string]any) map[string]any {
	scope := make(map[string]any, len(f.captured)+len(args))
	for name, value := range f.captured {
		scope[name] = value
	}
	for name, value := range args {
		scope[name] = value
	}
	return scope
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
	if function.tool != nil && len(function.tool.Params) > 0 && (function.bindArgs || hasNamedCallArguments(args)) {
		bound, err := bindCallArguments(function.name, function.tool.Params, args, function.defaults)
		if err != nil {
			return nil, err
		}
		return positionalArguments(function.tool.Params, bound), nil
	}

	positional := make([]any, 0, len(args))
	for _, arg := range args {
		if arg.Name != "" {
			return nil, fmt.Errorf("%s does not accept keyword arguments", function.name)
		}
		positional = append(positional, arg.Value)
	}
	return positional, nil
}

func hasNamedCallArguments(args []CallArgument) bool {
	for _, arg := range args {
		if arg.Name != "" {
			return true
		}
	}
	return false
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
				bound[param.Name] = cloneValue(value)
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
	Calls  []toolInvocation
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
			call, err := decodeActionToolInvocation(payload)
			if err != nil {
				return aiAction{}, err
			}
			return aiAction{Action: "call", Call: call}, nil
		case "call_many":
			calls, err := decodeActionToolInvocations(payload)
			if err != nil {
				return aiAction{}, err
			}
			return aiAction{Action: "call_many", Calls: calls}, nil
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
	if callsPayload, ok := payload["calls"]; ok {
		calls, err := decodeToolInvocations(callsPayload)
		if err != nil {
			return aiAction{}, err
		}
		return aiAction{Action: "call_many", Calls: calls}, nil
	}
	if toolCallsPayload, ok := payload["tool_calls"]; ok {
		calls, err := decodeToolInvocations(toolCallsPayload)
		if err != nil {
			return aiAction{}, err
		}
		return aiAction{Action: "call_many", Calls: calls}, nil
	}
	if functionName, ok := payload["function"].(string); ok {
		args, err := decodeToolInvocationArguments(payload)
		if err != nil {
			return aiAction{}, err
		}
		return aiAction{
			Action: "call",
			Call: &toolInvocation{
				Name:      functionName,
				Arguments: args,
			},
		}, nil
	}
	if hasToolInvocationPayload(payload) {
		call, err := decodeToolInvocation(payload)
		if err != nil {
			return aiAction{}, err
		}
		return aiAction{Action: "call", Call: call}, nil
	}

	return aiAction{}, fmt.Errorf("response did not include a valid action")
}

func decodeActionToolInvocation(payload map[string]any) (*toolInvocation, error) {
	if callPayload, ok := payload["call"]; ok && callPayload != nil {
		call, err := decodeToolInvocation(callPayload)
		if err == nil {
			return call, nil
		}
		if !hasToolInvocationPayload(payload) {
			return nil, err
		}
	}
	call, err := decodeToolInvocation(payload)
	if err == nil {
		return call, nil
	}
	if synthetic, ok := decodeSyntheticToolInvocation(payload); ok {
		return synthetic, nil
	}
	return nil, err
}

func decodeActionToolInvocations(payload map[string]any) ([]toolInvocation, error) {
	if callsPayload, ok := payload["calls"]; ok {
		return decodeToolInvocations(callsPayload)
	}
	if toolCallsPayload, ok := payload["tool_calls"]; ok {
		return decodeToolInvocations(toolCallsPayload)
	}
	return nil, fmt.Errorf("tool calls were not an array")
}

func hasToolInvocationPayload(payload map[string]any) bool {
	if hasToolInvocationName(payload) {
		return true
	}
	_, ok := nestedToolInvocationPayload(payload)
	return ok
}

func firstPresentValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func decodeToolInvocation(value any) (*toolInvocation, error) {
	callMap, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool call was not an object")
	}
	if nestedPayload, ok := nestedToolInvocationPayload(callMap); ok {
		return decodeToolInvocation(nestedPayload)
	}
	name, ok := toolInvocationName(callMap)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("tool call was missing a function name")
	}
	arguments, err := decodeToolInvocationArguments(callMap)
	if err != nil {
		return nil, err
	}
	return &toolInvocation{Name: name, Arguments: arguments}, nil
}

func decodeToolInvocations(value any) ([]toolInvocation, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("tool calls were not an array")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("tool calls array was empty")
	}

	calls := make([]toolInvocation, 0, len(items))
	for _, item := range items {
		call, err := decodeToolInvocation(item)
		if err != nil {
			return nil, err
		}
		calls = append(calls, *call)
	}
	return calls, nil
}

func decodeToolArguments(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return map[string]any{}, nil
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return nil, fmt.Errorf("tool call arguments: decode JSON string: %w", err)
		}
		if decoded == nil {
			return map[string]any{}, nil
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("tool call arguments: unsupported type %T", value)
	}
}

func nestedToolInvocationPayload(payload map[string]any) (map[string]any, bool) {
	if functionPayload, ok := payload["function"].(map[string]any); ok {
		return functionPayload, true
	}
	if hasToolInvocationName(payload) {
		return nil, false
	}
	for key, value := range payload {
		if !isToolArgumentKey(key) {
			continue
		}
		nested, ok := value.(map[string]any)
		if ok && hasToolInvocationName(nested) {
			return nested, true
		}
	}
	return nil, false
}

func hasToolInvocationName(payload map[string]any) bool {
	_, ok := toolInvocationName(payload)
	return ok
}

func toolInvocationName(payload map[string]any) (string, bool) {
	for _, key := range []string{"name", "function"} {
		name, ok := payload[key].(string)
		if ok && strings.TrimSpace(name) != "" {
			return name, true
		}
	}
	for _, prefix := range []string{"tool", "action", "function", "helper", "call"} {
		key := prefix + "_name"
		name, ok := payload[key].(string)
		if ok && strings.TrimSpace(name) != "" {
			return name, true
		}
	}
	return "", false
}

func decodeToolInvocationArguments(payload map[string]any) (map[string]any, error) {
	if key, explicit := explicitToolArgumentsValue(payload); explicit != nil {
		arguments, err := decodeToolArguments(explicit)
		if err == nil {
			return arguments, nil
		}
		if isScalarToolArgumentValue(explicit) {
			return map[string]any{scalarToolArgumentKey(key): explicit}, nil
		}
		return nil, err
	}

	inline := make(map[string]any)
	for key, value := range payload {
		if isToolInvocationMetadataKey(key) {
			continue
		}
		inline[key] = value
	}
	if len(inline) == 0 {
		return map[string]any{}, nil
	}
	return inline, nil
}

func isToolInvocationMetadataKey(key string) bool {
	switch key {
	case "name", "function", "action", "id", "type", "action_id", "action_type", "call_id", "call_type":
		return true
	default:
		return isToolNameKey(key) || isToolArgumentKey(key)
	}
}

func explicitToolArgumentsValue(payload map[string]any) (string, any) {
	for _, key := range []string{"arguments", "args", "parameters"} {
		if value, ok := payload[key]; ok {
			return key, value
		}
	}
	for _, prefix := range []string{"tool", "action", "function", "helper", "call"} {
		for _, suffix := range []string{"_input", "_args", "_params", "_parameters"} {
			key := prefix + suffix
			if value, ok := payload[key]; ok {
				return key, value
			}
		}
	}
	return "", nil
}

func isToolNameKey(key string) bool {
	if key == "name" || key == "function" {
		return true
	}
	if strings.HasSuffix(key, "_name") {
		return isToolAliasPrefix(strings.TrimSuffix(key, "_name"))
	}
	return false
}

func isToolArgumentKey(key string) bool {
	switch key {
	case "arguments", "args", "parameters":
		return true
	}
	for _, suffix := range []string{"_args", "_params", "_parameters", "_input"} {
		if strings.HasSuffix(key, suffix) {
			return isToolAliasPrefix(strings.TrimSuffix(key, suffix))
		}
	}
	return false
}

func isToolAliasPrefix(prefix string) bool {
	switch prefix {
	case "tool", "action", "function", "helper", "call":
		return true
	default:
		return false
	}
}

func isScalarToolArgumentValue(value any) bool {
	switch value.(type) {
	case nil, bool, float64, string:
		return true
	default:
		return false
	}
}

func scalarToolArgumentKey(key string) string {
	if strings.HasSuffix(key, "_input") {
		return "input"
	}
	if strings.HasSuffix(key, "_args") {
		return "args"
	}
	if strings.HasSuffix(key, "_params") || strings.HasSuffix(key, "_parameters") || key == "parameters" {
		return "parameters"
	}
	return "value"
}

func decodeSyntheticToolInvocation(payload map[string]any) (*toolInvocation, bool) {
	name := syntheticToolInvocationName(payload)
	if name == "" {
		return nil, false
	}

	arguments := make(map[string]any)
	for key, value := range payload {
		if isToolInvocationMetadataKey(key) {
			continue
		}
		arguments[key] = value
	}
	return &toolInvocation{Name: name, Arguments: arguments}, true
}

func syntheticToolInvocationName(payload map[string]any) string {
	for _, key := range []string{"action_type", "type"} {
		name, ok := payload[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(name)
		switch trimmed {
		case "", "function", "call":
			continue
		default:
			return trimmed
		}
	}
	return ""
}
