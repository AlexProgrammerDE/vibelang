package runtime

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"vibelang/internal/ast"
	"vibelang/internal/model"
)

type Config struct {
	Model        model.Client
	Stdout       io.Writer
	Stderr       io.Writer
	Trace        io.Writer
	MaxAISteps   int
	MaxCallDepth int
}

type Interpreter struct {
	mu            sync.RWMutex
	model         model.Client
	stdout        io.Writer
	stderr        io.Writer
	trace         io.Writer
	maxAISteps    int
	maxCallDepth  int
	globals       *Environment
	functions     map[string]*AIFunction
	tools         map[string]ToolCallable
	promptHelpers map[string]Callable
	promptCache   map[string]*compiledPrompt
	moduleCache   map[string]*loadedModule
	loadingModule map[string]bool
	sockets       map[string]*socketHandle
	tasks         map[string]*taskHandle
	channels      map[string]*channelHandle
	mutexes       map[string]*mutexHandle
	waitGroups    map[string]*safeWaitGroup
	servers       map[string]*httpServerHandle
	metrics       map[string]int64
	nextResource  int64
	telemetry     *telemetryManager
}

type controlSignal int

type aiCallFrame struct {
	Name      string
	Signature string
}

const (
	signalNone controlSignal = iota
	signalBreak
	signalContinue
)

func NewInterpreter(config Config) *Interpreter {
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.MaxAISteps == 0 {
		config.MaxAISteps = 8
	}
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 8
	}

	interpreter := &Interpreter{
		model:         config.Model,
		stdout:        config.Stdout,
		stderr:        config.Stderr,
		trace:         config.Trace,
		maxAISteps:    config.MaxAISteps,
		maxCallDepth:  config.MaxCallDepth,
		globals:       NewEnvironment(nil),
		functions:     make(map[string]*AIFunction),
		tools:         make(map[string]ToolCallable),
		promptHelpers: make(map[string]Callable),
		promptCache:   make(map[string]*compiledPrompt),
		moduleCache:   make(map[string]*loadedModule),
		loadingModule: make(map[string]bool),
		sockets:       make(map[string]*socketHandle),
		tasks:         make(map[string]*taskHandle),
		channels:      make(map[string]*channelHandle),
		mutexes:       make(map[string]*mutexHandle),
		waitGroups:    make(map[string]*safeWaitGroup),
		servers:       make(map[string]*httpServerHandle),
		metrics:       make(map[string]int64),
		telemetry:     newTelemetryManager(config.Stderr),
	}

	registerBuiltins(interpreter)
	return interpreter
}

func (i *Interpreter) Execute(ctx context.Context, program *ast.Program) error {
	workingDir, err := os.Getwd()
	if err != nil {
		return err
	}
	return i.executeProgram(ctx, program, workingDir, "")
}

func (i *Interpreter) ExecuteFile(ctx context.Context, program *ast.Program, sourcePath string) error {
	absolutePath, err := filepath.Abs(sourcePath)
	if err != nil {
		absolutePath = sourcePath
	}
	return i.executeProgram(ctx, program, filepath.Dir(absolutePath), absolutePath)
}

func (i *Interpreter) executeProgram(ctx context.Context, program *ast.Program, moduleDir, sourcePath string) error {
	env := NewEnvironment(i.globals)
	env.Define("__dir__", moduleDir)
	if sourcePath != "" {
		env.Define("__file__", sourcePath)
	}

	signal, err := i.executeBlock(ctx, env, program.Statements, moduleDir)
	if err != nil {
		return err
	}
	if signal != signalNone {
		return fmt.Errorf("unexpected control flow at top level")
	}
	return nil
}

func (i *Interpreter) executeBlock(ctx context.Context, env *Environment, statements []ast.Stmt, moduleDir string) (controlSignal, error) {
	for _, statement := range statements {
		signal, err := i.executeStatement(ctx, env, statement, moduleDir)
		if err != nil {
			return signalNone, err
		}
		if signal != signalNone {
			return signal, nil
		}
	}
	return signalNone, nil
}

func (i *Interpreter) executeStatement(ctx context.Context, env *Environment, statement ast.Stmt, moduleDir string) (controlSignal, error) {
	switch node := statement.(type) {
	case *ast.FunctionDef:
		defaults, err := i.evaluateParameterDefaults(ctx, env, node.Params)
		if err != nil {
			return signalNone, err
		}
		function, err := NewAIFunction(node, defaults, env.SnapshotValues())
		if err != nil {
			return signalNone, err
		}
		env.Define(node.Name, function)
		i.registerFunction(function)
		return signalNone, nil
	case *ast.MacroDef:
		defaults, err := i.evaluateParameterDefaults(ctx, env, node.Params)
		if err != nil {
			return signalNone, err
		}
		macro, err := NewAIMacro(node, defaults, env.SnapshotValues())
		if err != nil {
			return signalNone, err
		}
		env.Define(node.Name, macro)
		return signalNone, nil
	case *ast.ImportStmt:
		if err := i.executeImport(ctx, env, moduleDir, node); err != nil {
			return signalNone, err
		}
		return signalNone, nil
	case *ast.FromImportStmt:
		if err := i.executeFromImport(ctx, env, moduleDir, node); err != nil {
			return signalNone, err
		}
		return signalNone, nil
	case *ast.AssignStmt:
		value, err := i.evaluateExpression(ctx, env, node.Value)
		if err != nil {
			return signalNone, err
		}
		if err := i.assignValue(ctx, env, node.Target, value); err != nil {
			return signalNone, err
		}
		return signalNone, nil
	case *ast.ExprStmt:
		_, err := i.evaluateExpression(ctx, env, node.Expr)
		return signalNone, err
	case *ast.IfStmt:
		condition, err := i.evaluateCondition(ctx, env, node.Condition)
		if err != nil {
			return signalNone, err
		}
		if condition {
			return i.executeBlock(ctx, env, node.Then, moduleDir)
		}
		return i.executeBlock(ctx, env, node.Else, moduleDir)
	case *ast.MatchStmt:
		subject, err := i.evaluateExpression(ctx, env, node.Subject)
		if err != nil {
			return signalNone, err
		}
		for _, matchCase := range node.Cases {
			bindings := make(map[string]any)
			matched, err := matchPattern(matchCase.Pattern, subject, bindings)
			if err != nil {
				return signalNone, err
			}
			if !matched {
				continue
			}
			for name, value := range bindings {
				env.Set(name, value)
			}
			return i.executeBlock(ctx, env, matchCase.Body, moduleDir)
		}
		return signalNone, nil
	case *ast.WhileStmt:
		for {
			condition, err := i.evaluateCondition(ctx, env, node.Condition)
			if err != nil {
				return signalNone, err
			}
			if !condition {
				return signalNone, nil
			}
			signal, err := i.executeBlock(ctx, env, node.Body, moduleDir)
			if err != nil {
				return signalNone, err
			}
			switch signal {
			case signalNone:
			case signalBreak:
				return signalNone, nil
			case signalContinue:
				continue
			}
		}
	case *ast.TryStmt:
		return i.executeTryStatement(ctx, env, node, moduleDir)
	case *ast.ForStmt:
		iterable, err := i.evaluateExpression(ctx, env, node.Iterable)
		if err != nil {
			return signalNone, err
		}
		values, err := iterableValues(iterable)
		if err != nil {
			return signalNone, err
		}
		for _, value := range values {
			env.Set(node.Name, value)
			signal, err := i.executeBlock(ctx, env, node.Body, moduleDir)
			if err != nil {
				return signalNone, err
			}
			switch signal {
			case signalNone:
			case signalBreak:
				return signalNone, nil
			case signalContinue:
				continue
			}
		}
		return signalNone, nil
	case *ast.BreakStmt:
		return signalBreak, nil
	case *ast.ContinueStmt:
		return signalContinue, nil
	case *ast.PassStmt:
		return signalNone, nil
	default:
		return signalNone, fmt.Errorf("unsupported statement type %T", statement)
	}
}

func (i *Interpreter) executeTryStatement(ctx context.Context, env *Environment, statement *ast.TryStmt, moduleDir string) (controlSignal, error) {
	signal, err := i.executeBlock(ctx, env, statement.Body, moduleDir)
	if err != nil {
		if len(statement.Except) == 0 {
			return i.executeFinally(ctx, env, statement.Finally, moduleDir, signalNone, err)
		}
		if statement.ErrorName != "" {
			env.Set(statement.ErrorName, err.Error())
		}
		signal, err = i.executeBlock(ctx, env, statement.Except, moduleDir)
		if err != nil {
			return i.executeFinally(ctx, env, statement.Finally, moduleDir, signalNone, err)
		}
		return i.executeFinally(ctx, env, statement.Finally, moduleDir, signal, nil)
	}

	return i.executeFinally(ctx, env, statement.Finally, moduleDir, signal, nil)
}

func (i *Interpreter) executeFinally(ctx context.Context, env *Environment, body []ast.Stmt, moduleDir string, priorSignal controlSignal, priorErr error) (controlSignal, error) {
	if len(body) == 0 {
		return priorSignal, priorErr
	}
	finalSignal, err := i.executeBlock(ctx, env, body, moduleDir)
	if err != nil {
		return signalNone, err
	}
	if finalSignal != signalNone {
		return finalSignal, nil
	}
	return priorSignal, priorErr
}

func (i *Interpreter) assignValue(ctx context.Context, env *Environment, target ast.Expr, value any) error {
	switch node := target.(type) {
	case *ast.Identifier:
		env.Set(node.Name, value)
		return nil
	case *ast.IndexExpr:
		left, err := i.evaluateExpression(ctx, env, node.Left)
		if err != nil {
			return err
		}
		index, err := i.evaluateExpression(ctx, env, node.Index)
		if err != nil {
			return err
		}
		switch container := left.(type) {
		case []any:
			position, err := normalizeSequenceIndex(index, len(container), "list")
			if err != nil {
				return err
			}
			container[position] = value
			return nil
		case map[string]any:
			container[stringify(index)] = value
			return nil
		default:
			return fmt.Errorf("cannot assign through %s", typeName(left))
		}
	case *ast.MemberExpr:
		left, err := i.evaluateExpression(ctx, env, node.Left)
		if err != nil {
			return err
		}
		container, ok := left.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot assign member %q on %s", node.Name, typeName(left))
		}
		container[node.Name] = value
		return nil
	default:
		return fmt.Errorf("invalid assignment target")
	}
}

func (i *Interpreter) evaluateCondition(ctx context.Context, env *Environment, expression ast.Expr) (bool, error) {
	if prompt, ok := expression.(*ast.PromptExpr); ok {
		value, err := i.invokePromptExpression(ctx, env, prompt, "bool")
		if err != nil {
			return false, err
		}
		boolean, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("prompt condition did not return a bool")
		}
		return boolean, nil
	}

	value, err := i.evaluateExpression(ctx, env, expression)
	if err != nil {
		return false, err
	}
	return truthy(value), nil
}

func matchPattern(pattern ast.Expr, subject any, bindings map[string]any) (bool, error) {
	switch node := pattern.(type) {
	case *ast.Identifier:
		if node.Name == "_" {
			return true, nil
		}
		if value, exists := bindings[node.Name]; exists {
			return reflect.DeepEqual(value, subject), nil
		}
		bindings[node.Name] = subject
		return true, nil
	case *ast.Literal:
		return reflect.DeepEqual(node.Value, subject), nil
	case *ast.UnaryExpr:
		value, ok := unaryPatternLiteral(node)
		if !ok {
			return false, fmt.Errorf("unsupported match pattern %T", pattern)
		}
		return reflect.DeepEqual(value, subject), nil
	case *ast.ListLiteral:
		values, ok := asList(subject)
		if !ok || len(values) != len(node.Elements) {
			return false, nil
		}
		for index, element := range node.Elements {
			matched, err := matchPattern(element, values[index], bindings)
			if err != nil || !matched {
				return matched, err
			}
		}
		return true, nil
	case *ast.DictLiteral:
		values, ok := asMap(subject)
		if !ok {
			return false, nil
		}
		for _, item := range node.Items {
			key, err := patternKey(item.Key)
			if err != nil {
				return false, err
			}
			value, exists := values[key]
			if !exists {
				return false, nil
			}
			matched, err := matchPattern(item.Value, value, bindings)
			if err != nil || !matched {
				return matched, err
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported match pattern %T", pattern)
	}
}

func unaryPatternLiteral(pattern *ast.UnaryExpr) (any, bool) {
	if pattern.Operator != "-" {
		return nil, false
	}
	literal, ok := pattern.Right.(*ast.Literal)
	if !ok {
		return nil, false
	}
	switch value := literal.Value.(type) {
	case int64:
		return -value, true
	case float64:
		return -value, true
	default:
		return nil, false
	}
}

func patternKey(expression ast.Expr) (string, error) {
	switch node := expression.(type) {
	case *ast.Literal:
		return stringify(node.Value), nil
	case *ast.Identifier:
		if node.Name == "_" {
			return "", fmt.Errorf("wildcard cannot be used as a dict pattern key")
		}
		return node.Name, nil
	case *ast.UnaryExpr:
		value, ok := unaryPatternLiteral(node)
		if !ok {
			return "", fmt.Errorf("dict pattern keys must be literal values")
		}
		return stringify(value), nil
	default:
		return "", fmt.Errorf("dict pattern keys must be literal values")
	}
}

func (i *Interpreter) evaluateExpression(ctx context.Context, env *Environment, expression ast.Expr) (any, error) {
	switch node := expression.(type) {
	case *ast.Identifier:
		return env.Get(node.Name)
	case *ast.Literal:
		return node.Value, nil
	case *ast.PromptExpr:
		return i.invokePromptExpression(ctx, env, node, "any")
	case *ast.UnaryExpr:
		right, err := i.evaluateExpression(ctx, env, node.Right)
		if err != nil {
			return nil, err
		}
		switch node.Operator {
		case "-":
			if value, ok := asFloat(right); ok {
				if intValue, intOK := asInt(right); intOK {
					return -intValue, nil
				}
				return -value, nil
			}
			return nil, fmt.Errorf("operator '-' requires a number, got %s", typeName(right))
		case "not":
			return !truthy(right), nil
		default:
			return nil, fmt.Errorf("unsupported unary operator %q", node.Operator)
		}
	case *ast.BinaryExpr:
		if node.Operator == "and" {
			left, err := i.evaluateExpression(ctx, env, node.Left)
			if err != nil {
				return nil, err
			}
			if !truthy(left) {
				return left, nil
			}
			return i.evaluateExpression(ctx, env, node.Right)
		}
		if node.Operator == "or" {
			left, err := i.evaluateExpression(ctx, env, node.Left)
			if err != nil {
				return nil, err
			}
			if truthy(left) {
				return left, nil
			}
			return i.evaluateExpression(ctx, env, node.Right)
		}

		left, err := i.evaluateExpression(ctx, env, node.Left)
		if err != nil {
			return nil, err
		}
		right, err := i.evaluateExpression(ctx, env, node.Right)
		if err != nil {
			return nil, err
		}
		return evaluateBinary(node.Operator, left, right)
	case *ast.CallExpr:
		callee, err := i.evaluateExpression(ctx, env, node.Callee)
		if err != nil {
			return nil, err
		}
		callable, ok := callee.(Callable)
		if !ok {
			return nil, fmt.Errorf("%s is not callable", typeName(callee))
		}
		args := make([]CallArgument, 0, len(node.Arguments))
		for _, argExpr := range node.Arguments {
			value, err := i.evaluateExpression(ctx, env, argExpr.Value)
			if err != nil {
				return nil, err
			}
			args = append(args, CallArgument{
				Name:  argExpr.Name,
				Value: value,
			})
		}
		return callable.Call(ctx, i, args)
	case *ast.MacroCallExpr:
		callee, err := i.evaluateExpression(ctx, env, node.Callee)
		if err != nil {
			return nil, err
		}
		macro, ok := callee.(MacroCallable)
		if !ok {
			return nil, fmt.Errorf("%s is not a macro", typeName(callee))
		}
		args := make([]CallArgument, 0, len(node.Arguments))
		for _, argExpr := range node.Arguments {
			value, err := i.evaluateExpression(ctx, env, argExpr.Value)
			if err != nil {
				return nil, err
			}
			args = append(args, CallArgument{
				Name:  argExpr.Name,
				Value: value,
			})
		}
		return macro.Expand(ctx, i, env, args)
	case *ast.IndexExpr:
		left, err := i.evaluateExpression(ctx, env, node.Left)
		if err != nil {
			return nil, err
		}
		index, err := i.evaluateExpression(ctx, env, node.Index)
		if err != nil {
			return nil, err
		}
		switch container := left.(type) {
		case []any:
			position, err := normalizeSequenceIndex(index, len(container), "list")
			if err != nil {
				return nil, err
			}
			return container[position], nil
		case string:
			runes := []rune(container)
			position, err := normalizeSequenceIndex(index, len(runes), "string")
			if err != nil {
				return nil, err
			}
			return string(runes[position]), nil
		case map[string]any:
			value, ok := container[stringify(index)]
			if !ok {
				return nil, fmt.Errorf("key %q does not exist", stringify(index))
			}
			return value, nil
		default:
			return nil, fmt.Errorf("cannot index %s", typeName(left))
		}
	case *ast.SliceExpr:
		left, err := i.evaluateExpression(ctx, env, node.Left)
		if err != nil {
			return nil, err
		}

		start, err := i.evaluateOptionalExpression(ctx, env, node.Start)
		if err != nil {
			return nil, err
		}
		end, err := i.evaluateOptionalExpression(ctx, env, node.End)
		if err != nil {
			return nil, err
		}
		step, err := i.evaluateOptionalExpression(ctx, env, node.Step)
		if err != nil {
			return nil, err
		}

		switch container := left.(type) {
		case []any:
			startIndex, endIndex, stepValue, err := normalizeSliceBounds(len(container), start, end, step)
			if err != nil {
				return nil, err
			}
			return sliceList(container, startIndex, endIndex, stepValue), nil
		case string:
			runes := []rune(container)
			startIndex, endIndex, stepValue, err := normalizeSliceBounds(len(runes), start, end, step)
			if err != nil {
				return nil, err
			}
			return sliceString(runes, startIndex, endIndex, stepValue), nil
		default:
			return nil, fmt.Errorf("cannot slice %s", typeName(left))
		}
	case *ast.MemberExpr:
		left, err := i.evaluateExpression(ctx, env, node.Left)
		if err != nil {
			return nil, err
		}
		container, ok := left.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cannot access member %q on %s", node.Name, typeName(left))
		}
		value, ok := container[node.Name]
		if !ok {
			return nil, fmt.Errorf("member %q does not exist", node.Name)
		}
		return value, nil
	case *ast.ListLiteral:
		values := make([]any, 0, len(node.Elements))
		for _, element := range node.Elements {
			value, err := i.evaluateExpression(ctx, env, element)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case *ast.ListComprehensionExpr:
		iterable, err := i.evaluateExpression(ctx, env, node.Iterable)
		if err != nil {
			return nil, err
		}
		items, err := iterableValues(iterable)
		if err != nil {
			return nil, err
		}
		compEnv := NewEnvironment(env)
		values := make([]any, 0, len(items))
		for _, item := range items {
			compEnv.Set(node.Name, item)
			if node.Condition != nil {
				include, err := i.evaluateCondition(ctx, compEnv, node.Condition)
				if err != nil {
					return nil, err
				}
				if !include {
					continue
				}
			}
			value, err := i.evaluateExpression(ctx, compEnv, node.Element)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case *ast.DictLiteral:
		values := make(map[string]any, len(node.Items))
		for _, item := range node.Items {
			key, err := i.evaluateExpression(ctx, env, item.Key)
			if err != nil {
				return nil, err
			}
			value, err := i.evaluateExpression(ctx, env, item.Value)
			if err != nil {
				return nil, err
			}
			values[stringify(key)] = value
		}
		return values, nil
	case *ast.DictComprehensionExpr:
		iterable, err := i.evaluateExpression(ctx, env, node.Iterable)
		if err != nil {
			return nil, err
		}
		items, err := iterableValues(iterable)
		if err != nil {
			return nil, err
		}
		compEnv := NewEnvironment(env)
		values := make(map[string]any, len(items))
		for _, item := range items {
			compEnv.Set(node.Name, item)
			if node.Condition != nil {
				include, err := i.evaluateCondition(ctx, compEnv, node.Condition)
				if err != nil {
					return nil, err
				}
				if !include {
					continue
				}
			}
			key, err := i.evaluateExpression(ctx, compEnv, node.Key)
			if err != nil {
				return nil, err
			}
			value, err := i.evaluateExpression(ctx, compEnv, node.Value)
			if err != nil {
				return nil, err
			}
			values[stringify(key)] = value
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported expression type %T", expression)
	}
}

func (i *Interpreter) evaluateOptionalExpression(ctx context.Context, env *Environment, expression ast.Expr) (any, error) {
	if expression == nil {
		return nil, nil
	}
	return i.evaluateExpression(ctx, env, expression)
}

func sliceList(values []any, start, end, step int) []any {
	if len(values) == 0 {
		return []any{}
	}

	result := make([]any, 0)
	if step > 0 {
		for index := start; index < end; index += step {
			result = append(result, values[index])
		}
		return result
	}

	for index := start; index > end; index += step {
		result = append(result, values[index])
	}
	return result
}

func sliceString(runes []rune, start, end, step int) string {
	if len(runes) == 0 {
		return ""
	}

	var builder strings.Builder
	if step > 0 {
		for index := start; index < end; index += step {
			builder.WriteRune(runes[index])
		}
		return builder.String()
	}

	for index := start; index > end; index += step {
		builder.WriteRune(runes[index])
	}
	return builder.String()
}

func (i *Interpreter) invokePromptExpression(ctx context.Context, env *Environment, prompt *ast.PromptExpr, returnType string) (any, error) {
	settings, body, err := parseAIBody(prompt.Text)
	if err != nil {
		return nil, err
	}
	instructions, err := i.renderPromptText(ctx, body, env.SnapshotValues())
	if err != nil {
		return nil, err
	}
	task := &AIFunction{
		Def: &ast.FunctionDef{
			Line:       prompt.Line,
			Name:       fmt.Sprintf("__prompt_line_%d", prompt.Line),
			ReturnType: ast.TypeRef{Expr: returnType},
			Body:       prompt.Text,
		},
		defaults:     map[string]any{},
		instructions: body,
		directives:   settings,
	}
	return i.invokeAITask(ctx, task, env.SnapshotValues(), instructions, 0, "", nil, settings)
}

func (i *Interpreter) invokeAIFunction(ctx context.Context, function *AIFunction, args map[string]any, depth int, chain []aiCallFrame) (any, error) {
	scope := function.scope(args)
	instructions, err := i.renderPromptText(ctx, function.instructions, scope)
	if err != nil {
		return nil, err
	}
	return i.invokeAITask(ctx, function, scope, instructions, depth, function.Name(), extendAIChain(chain, function.Name(), args), function.directives)
}

func (i *Interpreter) invokeAITask(ctx context.Context, function *AIFunction, args map[string]any, instructions string, depth int, excludeTool string, chain []aiCallFrame, directives aiDirectiveConfig) (any, error) {
	if i.model == nil {
		return nil, fmt.Errorf("no model client configured")
	}
	if depth >= i.maxCallDepth {
		return nil, fmt.Errorf("%s exceeded the maximum AI call depth of %d", function.Name(), i.maxCallDepth)
	}

	history := make([]ToolEvent, 0)
	tools := i.toolSpecs(excludeTool, directives)
	maxSteps := i.maxAISteps
	if directives.MaxSteps != nil {
		maxSteps = *directives.MaxSteps
	}

	for step := 0; step < maxSteps; step++ {
		prompt, err := buildPrompt(function, instructions, args, tools, history)
		if err != nil {
			return nil, err
		}

		response, err := i.model.Generate(ctx, model.Request{
			System:      aiSystemPrompt(),
			Prompt:      prompt,
			JSONSchema:  aiActionSchema(),
			Temperature: directives.Temperature,
			MaxTokens:   directives.MaxTokens,
		})
		i.incrementMetric("ai_model_requests_total", 1)
		if err != nil {
			i.incrementMetric("ai_model_request_errors_total", 1)
			return nil, fmt.Errorf("%s model request failed: %w", function.Name(), err)
		}
		i.tracef("%s raw model response: %s", function.Name(), response.Text)

		action, err := decodeAIAction(response.Text)
		if err != nil {
			return nil, fmt.Errorf("%s returned invalid JSON action: %w", function.Name(), err)
		}

		switch action.Action {
		case "return":
			value, err := Coerce(function.Def.ReturnType.String(), action.Value)
			if err != nil {
				return nil, fmt.Errorf("%s returned a value that does not match %s: %w", function.Name(), function.Def.ReturnType.String(), err)
			}
			i.incrementMetric("ai_returns_total", 1)
			i.tracef("%s returning %s", function.Name(), jsonString(value))
			return value, nil
		case "call":
			if action.Call == nil {
				return nil, fmt.Errorf("%s requested a helper call without call details", function.Name())
			}
			callee, ok := i.lookupTool(action.Call.Name)
			if !ok {
				return nil, fmt.Errorf("%s requested unknown helper %q", function.Name(), action.Call.Name)
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
			rejection := ""
			if !directives.allowsTool(action.Call.Name) {
				rejection = fmt.Sprintf("the helper %s is not enabled for this AI function", action.Call.Name)
			} else {
				rejection = rejectAIToolCall(action.Call.Name, bound, excludeTool, chain)
			}
			if rejection != "" {
				if repeatedRejectedToolCall(history, action.Call.Name, bound) {
					i.incrementMetric("ai_tool_call_retries_blocked_total", 1)
					return nil, fmt.Errorf("%s repeatedly requested the rejected helper %s(%s): %s", function.Name(), action.Call.Name, jsonString(bound), rejection)
				}
				i.incrementMetric("ai_tool_call_rejections_total", 1)
				i.tracef("%s rejected call to %s with %s: %s", function.Name(), action.Call.Name, jsonString(bound), rejection)
				history = append(history, ToolEvent{
					Name:      action.Call.Name,
					Arguments: bound,
					Error:     rejection,
					Rejected:  true,
				})
				continue
			}
			i.incrementMetric("ai_tool_calls_total", 1)
			i.tracef("%s calling %s with %s", function.Name(), action.Call.Name, jsonString(bound))
			result, err := i.invokeTool(ctx, callee, bound, depth+1, chain)
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
			return nil, fmt.Errorf("%s returned unsupported action %q", function.Name(), action.Action)
		}
	}

	return nil, fmt.Errorf("%s exceeded the maximum AI tool steps of %d", function.Name(), maxSteps)
}

func (i *Interpreter) invokeTool(ctx context.Context, callable ToolCallable, bound map[string]any, depth int, chain []aiCallFrame) (any, error) {
	switch tool := callable.(type) {
	case *AIFunction:
		return i.invokeAIFunction(ctx, tool, bound, depth, chain)
	default:
		spec := callable.ToolSpec()
		values := positionalArguments(spec.Params, bound)
		args := make([]CallArgument, 0, len(values))
		for _, value := range values {
			args = append(args, CallArgument{Value: value})
		}
		return callable.Call(ctx, i, args)
	}
}

func extendAIChain(chain []aiCallFrame, name string, args map[string]any) []aiCallFrame {
	next := make([]aiCallFrame, 0, len(chain)+1)
	next = append(next, chain...)
	next = append(next, aiCallFrame{
		Name:      name,
		Signature: aiCallSignature(name, args),
	})
	return next
}

func aiCallSignature(name string, args map[string]any) string {
	return name + ":" + jsonString(args)
}

func rejectAIToolCall(name string, args map[string]any, excludeTool string, chain []aiCallFrame) string {
	if excludeTool != "" && name == excludeTool {
		return "the current AI function cannot call itself; return a value or choose a different helper"
	}

	signature := aiCallSignature(name, args)
	for _, frame := range chain {
		if frame.Signature == signature {
			return fmt.Sprintf("the AI call %s(%s) is already active; repeating it would recurse indefinitely", name, jsonString(args))
		}
	}
	return ""
}

func repeatedRejectedToolCall(history []ToolEvent, name string, args map[string]any) bool {
	signature := aiCallSignature(name, args)
	for _, event := range history {
		if !event.Rejected {
			continue
		}
		if aiCallSignature(event.Name, event.Arguments) == signature {
			return true
		}
	}
	return false
}

func (i *Interpreter) evaluateParameterDefaults(ctx context.Context, env *Environment, params []ast.Param) (map[string]any, error) {
	defaults := make(map[string]any)
	for _, param := range params {
		if param.Default == nil {
			continue
		}
		value, err := i.evaluateExpression(ctx, env, param.Default)
		if err != nil {
			return nil, fmt.Errorf("evaluate default for %q: %w", param.Name, err)
		}
		coerced, err := Coerce(param.Type.String(), value)
		if err != nil {
			return nil, fmt.Errorf("default for %q: %w", param.Name, err)
		}
		defaults[param.Name] = coerced
	}
	return defaults, nil
}

func (i *Interpreter) tracef(format string, args ...any) {
	if i.trace == nil {
		return
	}
	fmt.Fprintf(i.trace, format+"\n", args...)
}

func namedCallArguments(bound map[string]any) []CallArgument {
	args := make([]CallArgument, 0, len(bound))
	for name, value := range bound {
		args = append(args, CallArgument{Name: name, Value: value})
	}
	return args
}

func evaluateBinary(operator string, left, right any) (any, error) {
	switch operator {
	case "+":
		if leftString, ok := left.(string); ok {
			rightString, rightOK := right.(string)
			if !rightOK {
				return nil, fmt.Errorf("cannot concatenate string with %s", typeName(right))
			}
			return leftString + rightString, nil
		}
		if leftList, ok := asList(left); ok {
			rightList, rightOK := asList(right)
			if !rightOK {
				return nil, fmt.Errorf("cannot concatenate list with %s", typeName(right))
			}
			result := make([]any, 0, len(leftList)+len(rightList))
			result = append(result, leftList...)
			result = append(result, rightList...)
			return result, nil
		}
		return numericOperation(left, right, func(a, b float64) float64 { return a + b }, func(a, b int64) int64 { return a + b })
	case "-":
		return numericOperation(left, right, func(a, b float64) float64 { return a - b }, func(a, b int64) int64 { return a - b })
	case "*":
		return numericOperation(left, right, func(a, b float64) float64 { return a * b }, func(a, b int64) int64 { return a * b })
	case "/":
		leftValue, leftOK := asFloat(left)
		rightValue, rightOK := asFloat(right)
		if !leftOK || !rightOK {
			return nil, fmt.Errorf("operator '/' requires numbers")
		}
		if rightValue == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return leftValue / rightValue, nil
	case "%":
		leftValue, leftOK := asInt(left)
		rightValue, rightOK := asInt(right)
		if !leftOK || !rightOK {
			return nil, fmt.Errorf("operator '%%' requires integers")
		}
		if rightValue == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return leftValue % rightValue, nil
	case "==":
		return reflect.DeepEqual(left, right), nil
	case "!=":
		return !reflect.DeepEqual(left, right), nil
	case "<", "<=", ">", ">=":
		return compareValues(operator, left, right)
	case "in":
		return containsValue(right, left)
	default:
		return nil, fmt.Errorf("unsupported binary operator %q", operator)
	}
}

func numericOperation(left, right any, floatOp func(float64, float64) float64, intOp func(int64, int64) int64) (any, error) {
	if leftInt, leftOK := asInt(left); leftOK {
		if rightInt, rightOK := asInt(right); rightOK {
			return intOp(leftInt, rightInt), nil
		}
	}
	leftFloat, leftOK := asFloat(left)
	rightFloat, rightOK := asFloat(right)
	if !leftOK || !rightOK {
		return nil, fmt.Errorf("operator requires numbers")
	}
	result := floatOp(leftFloat, rightFloat)
	if math.Trunc(result) == result {
		return result, nil
	}
	return result, nil
}

func compareValues(operator string, left, right any) (bool, error) {
	if leftFloat, leftOK := asFloat(left); leftOK {
		rightFloat, rightOK := asFloat(right)
		if !rightOK {
			return false, fmt.Errorf("cannot compare %s with %s", typeName(left), typeName(right))
		}
		switch operator {
		case "<":
			return leftFloat < rightFloat, nil
		case "<=":
			return leftFloat <= rightFloat, nil
		case ">":
			return leftFloat > rightFloat, nil
		case ">=":
			return leftFloat >= rightFloat, nil
		}
	}

	leftString, leftOK := left.(string)
	rightString, rightRightOK := right.(string)
	if leftOK && rightRightOK {
		switch operator {
		case "<":
			return leftString < rightString, nil
		case "<=":
			return leftString <= rightString, nil
		case ">":
			return leftString > rightString, nil
		case ">=":
			return leftString >= rightString, nil
		}
	}

	return false, fmt.Errorf("cannot compare %s with %s", typeName(left), typeName(right))
}

func (i *Interpreter) FunctionNames() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	names := make([]string, 0, len(i.functions))
	for name := range i.functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
