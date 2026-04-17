package runtime

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"

	"vibelang/internal/ast"
	"vibelang/internal/model"
)

type Config struct {
	Model        model.Client
	Stdout       io.Writer
	Trace        io.Writer
	MaxAISteps   int
	MaxCallDepth int
}

type Interpreter struct {
	model        model.Client
	stdout       io.Writer
	trace        io.Writer
	maxAISteps   int
	maxCallDepth int
	globals      *Environment
	functions    map[string]*AIFunction
	tools        map[string]ToolCallable
}

type controlSignal int

const (
	signalNone controlSignal = iota
	signalBreak
	signalContinue
)

func NewInterpreter(config Config) *Interpreter {
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}
	if config.MaxAISteps == 0 {
		config.MaxAISteps = 8
	}
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 8
	}

	interpreter := &Interpreter{
		model:        config.Model,
		stdout:       config.Stdout,
		trace:        config.Trace,
		maxAISteps:   config.MaxAISteps,
		maxCallDepth: config.MaxCallDepth,
		globals:      NewEnvironment(nil),
		functions:    make(map[string]*AIFunction),
		tools:        make(map[string]ToolCallable),
	}

	registerBuiltins(interpreter)
	return interpreter
}

func (i *Interpreter) Execute(ctx context.Context, program *ast.Program) error {
	signal, err := i.executeBlock(ctx, i.globals, program.Statements)
	if err != nil {
		return err
	}
	if signal != signalNone {
		return fmt.Errorf("unexpected control flow at top level")
	}
	return nil
}

func (i *Interpreter) executeBlock(ctx context.Context, env *Environment, statements []ast.Stmt) (controlSignal, error) {
	for _, statement := range statements {
		signal, err := i.executeStatement(ctx, env, statement)
		if err != nil {
			return signalNone, err
		}
		if signal != signalNone {
			return signal, nil
		}
	}
	return signalNone, nil
}

func (i *Interpreter) executeStatement(ctx context.Context, env *Environment, statement ast.Stmt) (controlSignal, error) {
	switch node := statement.(type) {
	case *ast.FunctionDef:
		function := NewAIFunction(node)
		env.Define(node.Name, function)
		i.globals.Define(node.Name, function)
		i.functions[node.Name] = function
		i.tools[node.Name] = function
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
			return i.executeBlock(ctx, env, node.Then)
		}
		return i.executeBlock(ctx, env, node.Else)
	case *ast.WhileStmt:
		for {
			condition, err := i.evaluateCondition(ctx, env, node.Condition)
			if err != nil {
				return signalNone, err
			}
			if !condition {
				return signalNone, nil
			}
			signal, err := i.executeBlock(ctx, env, node.Body)
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
			signal, err := i.executeBlock(ctx, env, node.Body)
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
			position, ok := asInt(index)
			if !ok {
				return fmt.Errorf("list index must be an integer")
			}
			if position < 0 || int(position) >= len(container) {
				return fmt.Errorf("list index %d out of range", position)
			}
			container[position] = value
			return nil
		case map[string]any:
			container[stringify(index)] = value
			return nil
		default:
			return fmt.Errorf("cannot assign through %s", typeName(left))
		}
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
				return false, nil
			}
			right, err := i.evaluateExpression(ctx, env, node.Right)
			if err != nil {
				return nil, err
			}
			return truthy(right), nil
		}
		if node.Operator == "or" {
			left, err := i.evaluateExpression(ctx, env, node.Left)
			if err != nil {
				return nil, err
			}
			if truthy(left) {
				return true, nil
			}
			right, err := i.evaluateExpression(ctx, env, node.Right)
			if err != nil {
				return nil, err
			}
			return truthy(right), nil
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
		args := make([]any, 0, len(node.Arguments))
		for _, argExpr := range node.Arguments {
			value, err := i.evaluateExpression(ctx, env, argExpr)
			if err != nil {
				return nil, err
			}
			args = append(args, value)
		}
		return callable.Call(ctx, i, args)
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
			position, ok := asInt(index)
			if !ok {
				return nil, fmt.Errorf("list index must be an integer")
			}
			if position < 0 || int(position) >= len(container) {
				return nil, fmt.Errorf("list index %d out of range", position)
			}
			return container[position], nil
		case string:
			position, ok := asInt(index)
			if !ok {
				return nil, fmt.Errorf("string index must be an integer")
			}
			runes := []rune(container)
			if position < 0 || int(position) >= len(runes) {
				return nil, fmt.Errorf("string index %d out of range", position)
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
	default:
		return nil, fmt.Errorf("unsupported expression type %T", expression)
	}
}

func (i *Interpreter) invokePromptExpression(ctx context.Context, env *Environment, prompt *ast.PromptExpr, returnType string) (any, error) {
	task := &AIFunction{
		Def: &ast.FunctionDef{
			Line:       prompt.Line,
			Name:       fmt.Sprintf("__prompt_line_%d", prompt.Line),
			ReturnType: ast.TypeRef{Expr: returnType},
			Body:       prompt.Text,
		},
	}
	return i.invokeAITask(ctx, task, env.SnapshotValues(), 0, "")
}

func (i *Interpreter) invokeAIFunction(ctx context.Context, function *AIFunction, args map[string]any, depth int) (any, error) {
	return i.invokeAITask(ctx, function, args, depth, function.Name())
}

func (i *Interpreter) invokeAITask(ctx context.Context, function *AIFunction, args map[string]any, depth int, excludeTool string) (any, error) {
	if i.model == nil {
		return nil, fmt.Errorf("no model client configured")
	}
	if depth >= i.maxCallDepth {
		return nil, fmt.Errorf("%s exceeded the maximum AI call depth of %d", function.Name(), i.maxCallDepth)
	}

	history := make([]ToolEvent, 0)
	tools := sortedToolSpecs(i.tools, excludeTool)

	for step := 0; step < i.maxAISteps; step++ {
		prompt, err := buildPrompt(function, args, tools, history)
		if err != nil {
			return nil, err
		}

		response, err := i.model.Generate(ctx, model.Request{Prompt: prompt})
		if err != nil {
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
			i.tracef("%s returning %s", function.Name(), jsonString(value))
			return value, nil
		case "call":
			if action.Call == nil {
				return nil, fmt.Errorf("%s requested a helper call without call details", function.Name())
			}
			callee, ok := i.tools[action.Call.Name]
			if !ok {
				return nil, fmt.Errorf("%s requested unknown helper %q", function.Name(), action.Call.Name)
			}
			spec := callee.ToolSpec()
			bound, err := bindNamedArguments(action.Call.Name, spec.Params, action.Call.Arguments)
			if err != nil {
				return nil, err
			}
			i.tracef("%s calling %s with %s", function.Name(), action.Call.Name, jsonString(bound))
			result, err := i.invokeTool(ctx, callee, bound, depth+1)
			if err != nil {
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

	return nil, fmt.Errorf("%s exceeded the maximum AI tool steps of %d", function.Name(), i.maxAISteps)
}

func (i *Interpreter) invokeTool(ctx context.Context, callable ToolCallable, bound map[string]any, depth int) (any, error) {
	switch tool := callable.(type) {
	case *AIFunction:
		return i.invokeAIFunction(ctx, tool, bound, depth)
	default:
		spec := callable.ToolSpec()
		return callable.Call(ctx, i, positionalArguments(spec.Params, bound))
	}
}

func (i *Interpreter) tracef(format string, args ...any) {
	if i.trace == nil {
		return
	}
	fmt.Fprintf(i.trace, format+"\n", args...)
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
	names := make([]string, 0, len(i.functions))
	for name := range i.functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
