package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vibelang/internal/ast"
)

func registerBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, &builtinFunction{name: "print", call: builtinPrint})
	registerBuiltin(interpreter, &builtinFunction{name: "fail", call: builtinFail})
	registerBuiltin(interpreter, promptToolBuiltin("len", builtinLen, "int", "Return the length of a string, list, or dict.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("str", builtinStr, "string", "Convert any value to a string.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("int", builtinInt, "int", "Convert a value to an integer.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("float", builtinFloat, "float", "Convert a value to a float.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("bool", builtinBool, "bool", "Convert a value to a bool.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("type", builtinType, "string", "Return the runtime type name for a value.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "tool_catalog",
		call: builtinToolCatalog,
		tool: &ToolSpec{
			Name:       "tool_catalog",
			ReturnType: ast.TypeRef{Expr: "list[dict]"},
			Body:       "Return the available helper functions, optionally filtered by a name prefix.",
			Params: []ast.Param{
				{Name: "prefix", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"\""},
			},
		},
		defaults: map[string]any{
			"prefix": "",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("tool_describe", builtinToolDescribe, "dict", "Return one helper function description by name, including params, return_type, and body.", ast.Param{Name: "name", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "range",
		call: builtinRange,
		tool: &ToolSpec{
			Name:       "range",
			ReturnType: ast.TypeRef{Expr: "list[int]"},
			Body:       "Return a list of integers from start (inclusive) to stop (exclusive), stepping by step.",
			Params: []ast.Param{
				{Name: "start", Type: ast.TypeRef{Expr: "int"}, DefaultText: "0"},
				{Name: "stop", Type: ast.TypeRef{Expr: "int"}},
				{Name: "step", Type: ast.TypeRef{Expr: "int"}, DefaultText: "1"},
			},
		},
		defaults: map[string]any{
			"start": int64(0),
			"step":  int64(1),
		},
		promptSafe: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("append", builtinAppend, "list", "Return a new list with one value appended.", ast.Param{Name: "list", Type: ast.TypeRef{Expr: "list"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("keys", builtinKeys, "list[string]", "Return the sorted keys from a dict.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, promptToolBuiltin("values", builtinValues, "list", "Return the dict values in sorted-key order.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, promptToolBuiltin("json", builtinJSON, "string", "Encode a value as JSON.", ast.Param{Name: "value"}))
	registerDataBuiltins(interpreter)
	registerTextBuiltins(interpreter)
	registerAICacheBuiltins(interpreter)
	registerBuiltin(interpreter, promptToolBuiltin("cwd", builtinCWD, "string", "Return the current working directory."))
	registerBuiltin(interpreter, promptToolBuiltin("file_exists", builtinFileExists, "bool", "Return true when the given path exists.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("read_file", builtinReadFile, "string", "Read a UTF-8 text file and return its contents.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("join_path", builtinJoinPath, "string", "Join path segments using the host filesystem separator.", ast.Param{Name: "parts", Type: ast.TypeRef{Expr: "list[string]"}}))
	registerBuiltin(interpreter, promptToolBuiltin("abs_path", builtinAbsPath, "string", "Return the absolute version of a path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("dirname", builtinDirname, "string", "Return the parent directory for a path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("basename", builtinBasename, "string", "Return the final path element.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("list_dir", builtinListDir, "list[string]", "Return the sorted directory entries for a path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("is_dir", builtinIsDir, "bool", "Return true when the path exists and is a directory.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("env", builtinEnv, "any", "Return the value of an environment variable, or none when it is missing.", ast.Param{Name: "name", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("lower", builtinLower, "string", "Convert text to lowercase.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("upper", builtinUpper, "string", "Convert text to uppercase.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("trim", builtinTrim, "string", "Trim leading and trailing whitespace from text.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("split", builtinSplit, "list[string]", "Split text by a separator.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "separator", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("join", builtinJoin, "string", "Join a list of values using a separator.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}, ast.Param{Name: "separator", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("replace", builtinReplace, "string", "Replace every occurrence of one substring with another.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "old", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "new", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("contains", builtinContains, "bool", "Return true when a string contains a substring, a list contains a value, or a dict contains a key.", ast.Param{Name: "container"}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("read_json", builtinReadJSON, "any", "Read a JSON file and decode it into vibelang values.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("read_yaml", builtinReadYAML, "any", "Read a YAML file and decode it into vibelang values.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("write_file", builtinWriteFile, "string", "Write text to a file, creating parent directories when needed. Return the written path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "content", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("delete_file", builtinDeleteFile, "bool", "Delete a file. Return true if a file was removed and false if it was already missing.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("make_dir", builtinMakeDir, "string", "Create a directory and any missing parents. Return the created path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("append_file", builtinAppendFile, "string", "Append text to a file, creating it and its parent directories when needed. Return the written path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "content", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, toolBuiltin("write_json", builtinWriteJSON, "string", "Write a value as formatted JSON, creating parent directories when needed. Return the written path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, toolBuiltin("write_yaml", builtinWriteYAML, "string", "Write a value as YAML, creating parent directories when needed. Return the written path.", ast.Param{Name: "path", Type: ast.TypeRef{Expr: "string"}}, ast.Param{Name: "value"}))
	interpreter.globals.Define("pi", math.Pi)
	interpreter.globals.Define("e", math.E)
	registerExtendedBuiltins(interpreter)
	registerObservabilityBuiltins(interpreter)
}

func registerBuiltin(interpreter *Interpreter, builtin *builtinFunction) {
	interpreter.globals.Define(builtin.name, builtin)
	interpreter.mu.Lock()
	defer interpreter.mu.Unlock()
	if builtin.tool != nil {
		interpreter.tools[builtin.name] = builtin
	}
	if builtin.promptSafe {
		interpreter.promptHelpers[builtin.name] = builtin
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

func promptToolBuiltin(name string, call func(context.Context, *Interpreter, []any) (any, error), returnType, body string, params ...ast.Param) *builtinFunction {
	builtin := toolBuiltin(name, call, returnType, body, params...)
	builtin.promptSafe = true
	return builtin
}

func builtinPrint(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, stringify(arg))
	}
	_, err := fmt.Fprintln(interpreter.stdout, joinWithSpace(parts))
	return nil, err
}

func builtinFail(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("fail", args, 1); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s", stringify(args[0]))
}

func builtinLen(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("len", args, 1); err != nil {
		return nil, err
	}
	switch value := args[0].(type) {
	case string:
		return int64(len([]rune(value))), nil
	case []any:
		return int64(len(value)), nil
	case map[string]any:
		return int64(len(value)), nil
	case *SetValue:
		return int64(value.Len()), nil
	default:
		return nil, fmt.Errorf("len does not support %s", typeName(args[0]))
	}
}

func builtinStr(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("str", args, 1); err != nil {
		return nil, err
	}
	return stringify(args[0]), nil
}

func builtinInt(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("int", args, 1); err != nil {
		return nil, err
	}
	return coerceInt(args[0])
}

func builtinFloat(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("float", args, 1); err != nil {
		return nil, err
	}
	return coerceFloat(args[0])
}

func builtinBool(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("bool", args, 1); err != nil {
		return nil, err
	}
	return coerceBool(args[0])
}

func builtinType(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("type", args, 1); err != nil {
		return nil, err
	}
	return typeName(args[0]), nil
}

func builtinToolCatalog(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("tool_catalog", args, 1); err != nil {
		return nil, err
	}
	prefix, err := requireString("tool_catalog", args[0], "prefix")
	if err != nil {
		return nil, err
	}
	return interpreter.toolCatalog(prefix), nil
}

func builtinToolDescribe(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("tool_describe", args, 1); err != nil {
		return nil, err
	}
	name, err := requireString("tool_describe", args[0], "name")
	if err != nil {
		return nil, err
	}
	detail, ok := interpreter.toolDescription(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return detail, nil
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

func (i *Interpreter) toolCatalog(prefix string) []any {
	i.mu.RLock()
	defer i.mu.RUnlock()

	names := make([]string, 0, len(i.tools))
	for name := range i.tools {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	catalog := make([]any, 0, len(names))
	for _, name := range names {
		if detail, ok := i.toolDescriptionLocked(name); ok {
			catalog = append(catalog, detail)
		}
	}
	return catalog
}

func (i *Interpreter) toolDescription(name string) (map[string]any, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.toolDescriptionLocked(name)
}

func (i *Interpreter) toolDescriptionLocked(name string) (map[string]any, bool) {
	callable, ok := i.tools[name]
	if !ok {
		return nil, false
	}
	spec := callable.ToolSpec()
	params := make([]any, 0, len(spec.Params))
	for _, param := range spec.Params {
		entry := map[string]any{
			"name": param.Name,
			"type": param.Type.String(),
		}
		if param.DefaultText != "" {
			entry["has_default"] = true
			entry["default"] = param.DefaultText
		} else {
			entry["has_default"] = false
		}
		params = append(params, entry)
	}

	_, promptSafe := i.promptHelpers[name]
	kind := "builtin"
	if _, ok := callable.(*AIFunction); ok {
		kind = "ai"
	}

	return map[string]any{
		"name":        spec.Name,
		"kind":        kind,
		"prompt_safe": promptSafe,
		"signature":   fmt.Sprintf("%s(%s) -> %s", spec.Name, formatParams(spec.Params), spec.ReturnType.String()),
		"params":      params,
		"return_type": spec.ReturnType.String(),
		"body":        spec.Body,
	}, true
}

func builtinAppend(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("append", args, 2); err != nil {
		return nil, err
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
	if err := expectArgCount("keys", args, 1); err != nil {
		return nil, err
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
	if err := expectArgCount("values", args, 1); err != nil {
		return nil, err
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
	if err := expectArgCount("json", args, 1); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(args[0])
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func builtinCWD(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("cwd", args, 0); err != nil {
		return nil, err
	}
	return os.Getwd()
}

func builtinFileExists(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("file_exists", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("file_exists", args[0], "path")
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	if statErr == nil {
		return true, nil
	}
	if os.IsNotExist(statErr) {
		return false, nil
	}
	return nil, statErr
}

func builtinReadFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("read_file", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("read_file", args[0], "path")
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return string(content), nil
}

func builtinWriteFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("write_file", args, 2); err != nil {
		return nil, err
	}
	path, err := requireString("write_file", args[0], "path")
	if err != nil {
		return nil, err
	}
	content, err := requireString("write_file", args[1], "content")
	if err != nil {
		return nil, err
	}
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return path, nil
}

func builtinDeleteFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("delete_file", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("delete_file", args[0], "path")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return nil, err
	}
	return true, nil
}

func builtinJoinPath(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("join_path", args, 1); err != nil {
		return nil, err
	}
	parts, err := requireStringList("join_path", args[0], "parts")
	if err != nil {
		return nil, err
	}
	return filepath.Join(parts...), nil
}

func builtinAbsPath(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("abs_path", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("abs_path", args[0], "path")
	if err != nil {
		return nil, err
	}
	return filepath.Abs(path)
}

func builtinDirname(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("dirname", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("dirname", args[0], "path")
	if err != nil {
		return nil, err
	}
	return filepath.Dir(path), nil
}

func builtinBasename(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("basename", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("basename", args[0], "path")
	if err != nil {
		return nil, err
	}
	return filepath.Base(path), nil
}

func builtinListDir(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("list_dir", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("list_dir", args[0], "path")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	result := make([]any, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	return result, nil
}

func builtinIsDir(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("is_dir", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("is_dir", args[0], "path")
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return nil, err
	}
	return info.IsDir(), nil
}

func builtinEnv(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("env", args, 1); err != nil {
		return nil, err
	}
	name, err := requireString("env", args[0], "name")
	if err != nil {
		return nil, err
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil, nil
	}
	return value, nil
}

func builtinLower(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("lower", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("lower", args[0], "text")
	if err != nil {
		return nil, err
	}
	return strings.ToLower(text), nil
}

func builtinUpper(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("upper", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("upper", args[0], "text")
	if err != nil {
		return nil, err
	}
	return strings.ToUpper(text), nil
}

func builtinTrim(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("trim", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("trim", args[0], "text")
	if err != nil {
		return nil, err
	}
	return strings.TrimSpace(text), nil
}

func builtinSplit(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("split", args, 2); err != nil {
		return nil, err
	}
	text, err := requireString("split", args[0], "text")
	if err != nil {
		return nil, err
	}
	separator, err := requireString("split", args[1], "separator")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(text, separator)
	result := make([]any, 0, len(parts))
	for _, part := range parts {
		result = append(result, part)
	}
	return result, nil
}

func builtinJoin(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("join", args, 2); err != nil {
		return nil, err
	}
	values, err := requireStringList("join", args[0], "values")
	if err != nil {
		return nil, err
	}
	separator, err := requireString("join", args[1], "separator")
	if err != nil {
		return nil, err
	}
	return strings.Join(values, separator), nil
}

func builtinReplace(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("replace", args, 3); err != nil {
		return nil, err
	}
	text, err := requireString("replace", args[0], "text")
	if err != nil {
		return nil, err
	}
	oldText, err := requireString("replace", args[1], "old")
	if err != nil {
		return nil, err
	}
	newText, err := requireString("replace", args[2], "new")
	if err != nil {
		return nil, err
	}
	return strings.ReplaceAll(text, oldText, newText), nil
}

func builtinContains(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("contains", args, 2); err != nil {
		return nil, err
	}
	return containsValue(args[0], args[1])
}

func builtinReadJSON(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("read_json", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("read_json", args[0], "path")
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, err
	}
	return normalizeJSONValue(value), nil
}

func builtinReadYAML(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("read_yaml", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("read_yaml", args[0], "path")
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseYAMLText(string(content))
}

func builtinMakeDir(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("make_dir", args, 1); err != nil {
		return nil, err
	}
	path, err := requireString("make_dir", args[0], "path")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return path, nil
}

func builtinAppendFile(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("append_file", args, 2); err != nil {
		return nil, err
	}
	path, err := requireString("append_file", args[0], "path")
	if err != nil {
		return nil, err
	}
	content, err := requireString("append_file", args[1], "content")
	if err != nil {
		return nil, err
	}
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		return nil, err
	}
	return path, nil
}

func builtinWriteJSON(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("write_json", args, 2); err != nil {
		return nil, err
	}
	path, err := requireString("write_json", args[0], "path")
	if err != nil {
		return nil, err
	}
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(args[1], "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return nil, err
	}
	return path, nil
}

func builtinWriteYAML(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("write_yaml", args, 2); err != nil {
		return nil, err
	}
	path, err := requireString("write_yaml", args[0], "path")
	if err != nil {
		return nil, err
	}
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}
	encoded, err := marshalYAMLValue(args[1])
	if err != nil {
		return nil, err
	}
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		encoded = append(encoded, '\n')
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return nil, err
	}
	return path, nil
}

func expectArgCount(name string, args []any, want int) error {
	if len(args) != want {
		return fmt.Errorf("%s expects %d argument(s), got %d", name, want, len(args))
	}
	return nil
}

func requireString(name string, value any, param string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s expects %s to be a string", name, param)
	}
	return text, nil
}

func requireStringList(name string, value any, param string) ([]string, error) {
	list, ok := asList(value)
	if !ok {
		return nil, fmt.Errorf("%s expects %s to be a list", name, param)
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		result = append(result, stringify(item))
	}
	return result, nil
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func normalizeJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = normalizeJSONValue(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(v))
		for _, item := range v {
			result = append(result, normalizeJSONValue(item))
		}
		return result
	case *SetValue:
		return normalizeJSONValue(v.Values())
	case float64:
		if math.Trunc(v) == v {
			return int64(v)
		}
		return v
	default:
		return v
	}
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
