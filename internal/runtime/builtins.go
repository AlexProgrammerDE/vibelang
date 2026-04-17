package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"vibelang/internal/ast"
)

func registerBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, &builtinFunction{name: "print", call: builtinPrint})
	registerBuiltin(interpreter, toolBuiltin("len", builtinLen, "int", "Return the length of a string, list, or dict.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("str", builtinStr, "string", "Convert any value to a string.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("int", builtinInt, "int", "Convert a value to an integer.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("float", builtinFloat, "float", "Convert a value to a float.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("bool", builtinBool, "bool", "Convert a value to a bool.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("type", builtinType, "string", "Return the runtime type name for a value.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, &builtinFunction{name: "range", call: builtinRange})
	registerBuiltin(interpreter, toolBuiltin("append", builtinAppend, "list", "Return a new list with one value appended.", ast.Param{Name: "list", Type: ast.TypeRef{Expr: "list"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("keys", builtinKeys, "list[string]", "Return the sorted keys from a dict.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, toolBuiltin("values", builtinValues, "list", "Return the dict values in sorted-key order.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, toolBuiltin("json", builtinJSON, "string", "Encode a value as JSON.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("cwd", builtinCWD, "string", "Return the current working directory."))
	registerBuiltin(interpreter, toolBuiltin("file_exists", builtinFileExists, "bool", "Return true when the given path exists.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("read_file", builtinReadFile, "string", "Read a UTF-8 text file and return its contents.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("write_file", builtinWriteFile, "string", "Write text to a file, creating parent directories when needed. Return the written path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "content", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("delete_file", builtinDeleteFile, "bool", "Delete a file. Return true if a file was removed and false if it was already missing.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
}

func registerBuiltin(interpreter *Interpreter, builtin *builtinFunction) {
	interpreter.globals.Define(builtin.name, builtin)
	if builtin.tool != nil {
		interpreter.tools[builtin.name] = builtin
	}
}

func toolBuiltin(name string, call func(context.Context, *Interpreter, []any) (any, error), returnType, body string, params ...ast.Param) *builtinFunction {
	return &builtinFunction{
		name: name,
		call: call,
		tool: &ToolSpec{
			Name:       name,
			Params:     params,
			ReturnType: ast.TypeRef{Expr: returnType},
			Body:       body,
		},
	}
}

func builtinPrint(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, stringify(arg))
	}
	_, err := fmt.Fprintln(interpreter.stdout, joinWithSpace(parts))
	return nil, err
}

func builtinLen(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("len expects 1 argument, got %d", len(args))
	}
	switch value := args[0].(type) {
	case string:
		return int64(len([]rune(value))), nil
	case []any:
		return int64(len(value)), nil
	case map[string]any:
		return int64(len(value)), nil
	default:
		return nil, fmt.Errorf("len does not support %s", typeName(args[0]))
	}
}

func builtinStr(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("str expects 1 argument, got %d", len(args))
	}
	return stringify(args[0]), nil
}

func builtinInt(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("int expects 1 argument, got %d", len(args))
	}
	return coerceInt(args[0])
}

func builtinFloat(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("float expects 1 argument, got %d", len(args))
	}
	return coerceFloat(args[0])
}

func builtinBool(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("bool expects 1 argument, got %d", len(args))
	}
	return coerceBool(args[0])
}

func builtinType(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("type expects 1 argument, got %d", len(args))
	}
	return typeName(args[0]), nil
}

func builtinRange(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("range expects 1 to 3 arguments, got %d", len(args))
	}

	var start int64
	var stop int64
	step := int64(1)
	var err error

	switch len(args) {
	case 1:
		stop, err = coerceInt(args[0])
	case 2:
		start, err = coerceInt(args[0])
		if err == nil {
			stop, err = coerceInt(args[1])
		}
	case 3:
		start, err = coerceInt(args[0])
		if err == nil {
			stop, err = coerceInt(args[1])
		}
		if err == nil {
			step, err = coerceInt(args[2])
		}
	}
	if err != nil {
		return nil, err
	}
	if step == 0 {
		return nil, fmt.Errorf("range step cannot be zero")
	}

	result := make([]any, 0)
	if step > 0 {
		for value := start; value < stop; value += step {
			result = append(result, value)
		}
	} else {
		for value := start; value > stop; value += step {
			result = append(result, value)
		}
	}
	return result, nil
}

func builtinAppend(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("append expects 2 arguments, got %d", len(args))
	}
	list, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("append expects a list as the first argument")
	}
	result := make([]any, 0, len(list)+1)
	result = append(result, list...)
	result = append(result, args[1])
	return result, nil
}

func builtinKeys(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("keys expects 1 argument, got %d", len(args))
	}
	dict, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("keys expects a dict")
	}
	keys := make([]string, 0, len(dict))
	for key := range dict {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]any, 0, len(keys))
	for _, key := range keys {
		result = append(result, key)
	}
	return result, nil
}

func builtinValues(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("values expects 1 argument, got %d", len(args))
	}
	dict, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("values expects a dict")
	}
	keys := make([]string, 0, len(dict))
	for key := range dict {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]any, 0, len(keys))
	for _, key := range keys {
		result = append(result, dict[key])
	}
	return result, nil
}

func builtinJSON(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("json expects 1 argument, got %d", len(args))
	}
	encoded, err := json.Marshal(args[0])
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func builtinCWD(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("cwd expects 0 arguments, got %d", len(args))
	}
	return os.Getwd()
}

func builtinFileExists(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("file_exists expects 1 argument, got %d", len(args))
	}
	path, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("file_exists expects a string path")
	}
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return nil, err
}

func builtinReadFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("read_file expects 1 argument, got %d", len(args))
	}
	path, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("read_file expects a string path")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return string(content), nil
}

func builtinWriteFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("write_file expects 2 arguments, got %d", len(args))
	}
	path, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("write_file expects a string path")
	}
	content, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("write_file expects string content")
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return path, nil
}

func builtinDeleteFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("delete_file expects 1 argument, got %d", len(args))
	}
	path, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("delete_file expects a string path")
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return nil, err
	}
	return true, nil
}

func joinWithSpace(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		result += " " + part
	}
	return result
}
