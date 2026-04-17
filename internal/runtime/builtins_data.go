package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"vibelang/internal/ast"
)

func registerDataBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("json_parse", builtinJSONParse, "any", "Parse a JSON string into vibelang values.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "json_pretty",
		call: builtinJSONPretty,
		tool: &ToolSpec{
			Name:       "json_pretty",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Encode a value as indented JSON with the given indentation string.",
			Params: []ast.Param{
				{Name: "value"},
				{Name: "indent", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"  \""},
			},
		},
		defaults: map[string]any{
			"indent": "  ",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("set", builtinSet, "set", "Create a set from the given list of values.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_values", builtinSetValues, "list", "Return the sorted values from a set.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_has", builtinSetHas, "bool", "Return true when a set contains the given value.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set_add", builtinSetAdd, "set", "Return a new set with one value added.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set_remove", builtinSetRemove, "set", "Return a new set with one value removed.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set_union", builtinSetUnion, "set", "Return the union of two sets.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_intersection", builtinSetIntersection, "set", "Return the intersection of two sets.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_difference", builtinSetDifference, "set", "Return the difference of two sets.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "set"}}))
}

func builtinJSONParse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("json_parse", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("json_parse", args[0], "text")
	if err != nil {
		return nil, err
	}

	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil, err
	}
	return normalizeJSONValue(value), nil
}

func builtinJSONPretty(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("json_pretty", args, 2); err != nil {
		return nil, err
	}
	indent, err := requireString("json_pretty", args[1], "indent")
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(normalizeJSONValue(args[0]), "", indent)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func builtinSet(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set", args, 1); err != nil {
		return nil, err
	}
	values, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("set expects values to be a list")
	}
	return NewSetValue(values), nil
}

func builtinSetValues(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_values", args, 1); err != nil {
		return nil, err
	}
	set, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_values expects a set")
	}
	return set.Values(), nil
}

func builtinSetHas(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_has", args, 2); err != nil {
		return nil, err
	}
	set, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_has expects a set")
	}
	return set.Has(args[1]), nil
}

func builtinSetAdd(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_add", args, 2); err != nil {
		return nil, err
	}
	set, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_add expects a set")
	}
	return set.Add(args[1]), nil
}

func builtinSetRemove(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_remove", args, 2); err != nil {
		return nil, err
	}
	set, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_remove expects a set")
	}
	return set.Remove(args[1]), nil
}

func builtinSetUnion(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_union", args, 2); err != nil {
		return nil, err
	}
	left, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_union expects left to be a set")
	}
	right, ok := asSet(args[1])
	if !ok {
		return nil, fmt.Errorf("set_union expects right to be a set")
	}
	return left.Union(right), nil
}

func builtinSetIntersection(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_intersection", args, 2); err != nil {
		return nil, err
	}
	left, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_intersection expects left to be a set")
	}
	right, ok := asSet(args[1])
	if !ok {
		return nil, fmt.Errorf("set_intersection expects right to be a set")
	}
	return left.Intersection(right), nil
}

func builtinSetDifference(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("set_difference", args, 2); err != nil {
		return nil, err
	}
	left, ok := asSet(args[0])
	if !ok {
		return nil, fmt.Errorf("set_difference expects left to be a set")
	}
	right, ok := asSet(args[1])
	if !ok {
		return nil, fmt.Errorf("set_difference expects right to be a set")
	}
	return left.Difference(right), nil
}
