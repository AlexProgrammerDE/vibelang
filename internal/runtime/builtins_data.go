package runtime

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"vibelang/internal/ast"
)

func registerDataBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("json_parse", builtinJSONParse, "any", "Parse a JSON string into vibelang values.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, promptToolBuiltin("yaml_parse", builtinYAMLParse, "any", "Parse a YAML string into vibelang values.", ast.Param{Name: "text", Type: ast.TypeRef{Expr: "string"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "csv_parse",
		call: builtinCSVParse,
		tool: &ToolSpec{
			Name:       "csv_parse",
			ReturnType: ast.TypeRef{Expr: "list"},
			Body:       "Parse CSV text into a list of dict rows when header is true, or a list of string lists when header is false.",
			Params: []ast.Param{
				{Name: "text", Type: ast.TypeRef{Expr: "string"}},
				{Name: "header", Type: ast.TypeRef{Expr: "bool"}, DefaultText: "true"},
			},
		},
		defaults: map[string]any{
			"header": true,
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "csv_stringify",
		call: builtinCSVStringify,
		tool: &ToolSpec{
			Name:       "csv_stringify",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Encode a list of dict rows or a list of string lists as CSV text. Dict rows use sorted columns unless columns is provided.",
			Params: []ast.Param{
				{Name: "rows", Type: ast.TypeRef{Expr: "list"}},
				{Name: "header", Type: ast.TypeRef{Expr: "bool"}, DefaultText: "true"},
				{Name: "columns", Type: ast.TypeRef{Expr: "list[string]"}, DefaultText: "[]"},
			},
		},
		defaults: map[string]any{
			"header":  true,
			"columns": []any{},
		},
		bindArgs:   true,
		promptSafe: true,
	})
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
	registerBuiltin(interpreter, promptToolBuiltin("yaml_stringify", builtinYAMLStringify, "string", "Encode a value as YAML.", ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set", builtinSet, "set", "Create a set from the given list of values.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_values", builtinSetValues, "list", "Return the sorted values from a set.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_has", builtinSetHas, "bool", "Return true when a set contains the given value.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set_add", builtinSetAdd, "set", "Return a new set with one value added.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set_remove", builtinSetRemove, "set", "Return a new set with one value removed.", ast.Param{Name: "set", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("set_union", builtinSetUnion, "set", "Return the union of two sets.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_intersection", builtinSetIntersection, "set", "Return the intersection of two sets.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("set_difference", builtinSetDifference, "set", "Return the difference of two sets.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "set"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "set"}}))
	registerBuiltin(interpreter, promptToolBuiltin("dict_has", builtinDictHas, "bool", "Return true when a dict contains the given key.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}, ast.Param{Name: "key"}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "dict_get",
		call: builtinDictGet,
		tool: &ToolSpec{
			Name:       "dict_get",
			ReturnType: ast.TypeRef{Expr: "any"},
			Body:       "Return dict[key] when the key exists, otherwise return the provided default value.",
			Params: []ast.Param{
				{Name: "dict", Type: ast.TypeRef{Expr: "dict"}},
				{Name: "key"},
				{Name: "default", DefaultText: "none"},
			},
		},
		defaults: map[string]any{
			"default": nil,
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("dict_items", builtinDictItems, "list[dict]", "Return the sorted key/value entries from a dict as a list of {key, value} dictionaries.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, promptToolBuiltin("dict_set", builtinDictSet, "dict", "Return a new dict with one key assigned to the given value.", ast.Param{Name: "dict", Type: ast.TypeRef{Expr: "dict"}}, ast.Param{Name: "key"}, ast.Param{Name: "value"}))
	registerBuiltin(interpreter, promptToolBuiltin("dict_merge", builtinDictMerge, "dict", "Return a new dict containing all keys from left and right, with right winning on conflicts.", ast.Param{Name: "left", Type: ast.TypeRef{Expr: "dict"}}, ast.Param{Name: "right", Type: ast.TypeRef{Expr: "dict"}}))
	registerBuiltin(interpreter, &builtinFunction{
		name: "enumerate",
		call: builtinEnumerate,
		tool: &ToolSpec{
			Name:       "enumerate",
			ReturnType: ast.TypeRef{Expr: "list[dict]"},
			Body:       "Return a list of dictionaries with index and value fields for each element in the input list.",
			Params: []ast.Param{
				{Name: "values", Type: ast.TypeRef{Expr: "list"}},
				{Name: "start", Type: ast.TypeRef{Expr: "int"}, DefaultText: "0"},
			},
		},
		defaults: map[string]any{
			"start": int64(0),
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "zip",
		call: builtinZip,
		tool: &ToolSpec{
			Name:       "zip",
			ReturnType: ast.TypeRef{Expr: "list[list]"},
			Body:       "Pair values from two lists into a list of two-item lists. By default it stops at the shorter list, or errors when strict is true and lengths differ.",
			Params: []ast.Param{
				{Name: "left", Type: ast.TypeRef{Expr: "list"}},
				{Name: "right", Type: ast.TypeRef{Expr: "list"}},
				{Name: "strict", Type: ast.TypeRef{Expr: "bool"}, DefaultText: "false"},
			},
		},
		defaults: map[string]any{
			"strict": false,
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "sorted",
		call: builtinSorted,
		tool: &ToolSpec{
			Name:       "sorted",
			ReturnType: ast.TypeRef{Expr: "list"},
			Body:       "Return a new sorted list. Mixed types are sorted deterministically by type and value.",
			Params: []ast.Param{
				{Name: "values", Type: ast.TypeRef{Expr: "list"}},
				{Name: "descending", Type: ast.TypeRef{Expr: "bool"}, DefaultText: "false"},
			},
		},
		defaults: map[string]any{
			"descending": false,
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("unique", builtinUnique, "list", "Return a new list with duplicate values removed while preserving the first occurrence of each value.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}))
	registerBuiltin(interpreter, promptToolBuiltin("sum", builtinSum, "any", "Return the numeric sum of a list of ints or floats.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}))
	registerBuiltin(interpreter, promptToolBuiltin("min", builtinMin, "any", "Return the smallest value in a non-empty list.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}))
	registerBuiltin(interpreter, promptToolBuiltin("max", builtinMax, "any", "Return the largest value in a non-empty list.", ast.Param{Name: "values", Type: ast.TypeRef{Expr: "list"}}))
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

func builtinCSVParse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("csv_parse", args, 2); err != nil {
		return nil, err
	}
	text, err := requireString("csv_parse", args[0], "text")
	if err != nil {
		return nil, err
	}
	header, err := requireBool("csv_parse", args[1], "header")
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if !header {
		rows := make([]any, 0, len(records))
		for _, record := range records {
			row := make([]any, 0, len(record))
			for _, value := range record {
				row = append(row, value)
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	if len(records) == 0 {
		return []any{}, nil
	}

	headers := csvHeaders(records[0])
	rows := make([]any, 0, len(records)-1)
	for _, record := range records[1:] {
		row := make(map[string]any, len(headers))
		for index, column := range headers {
			if index < len(record) {
				row[column] = record[index]
			} else {
				row[column] = ""
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
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

func builtinCSVStringify(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("csv_stringify", args, 3); err != nil {
		return nil, err
	}
	rows, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("csv_stringify expects rows to be a list")
	}
	header, err := requireBool("csv_stringify", args[1], "header")
	if err != nil {
		return nil, err
	}
	columns, err := requireStringList("csv_stringify", args[2], "columns")
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)

	if len(rows) == 0 {
		if header && len(columns) > 0 {
			if err := writer.Write(columns); err != nil {
				return nil, err
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, err
		}
		return buffer.String(), nil
	}

	if _, ok := asMap(rows[0]); ok {
		if len(columns) == 0 {
			columns = csvColumnsFromRows(rows)
		}
		if header {
			if err := writer.Write(columns); err != nil {
				return nil, err
			}
		}
		for _, rawRow := range rows {
			row, ok := asMap(rawRow)
			if !ok {
				return nil, fmt.Errorf("csv_stringify expects every row to be a dict when the first row is a dict")
			}
			record := make([]string, 0, len(columns))
			for _, column := range columns {
				record = append(record, stringify(row[column]))
			}
			if err := writer.Write(record); err != nil {
				return nil, err
			}
		}
	} else {
		if header && len(columns) > 0 {
			if err := writer.Write(columns); err != nil {
				return nil, err
			}
		}
		for _, rawRow := range rows {
			row, ok := asList(rawRow)
			if !ok {
				return nil, fmt.Errorf("csv_stringify expects every row to be a list when the first row is a list")
			}
			record := make([]string, 0, len(row))
			for _, value := range row {
				record = append(record, stringify(value))
			}
			if err := writer.Write(record); err != nil {
				return nil, err
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

func builtinYAMLParse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("yaml_parse", args, 1); err != nil {
		return nil, err
	}
	text, err := requireString("yaml_parse", args[0], "text")
	if err != nil {
		return nil, err
	}
	return parseYAMLText(text)
}

func csvHeaders(headerRow []string) []string {
	headers := make([]string, 0, len(headerRow))
	seen := make(map[string]int, len(headerRow))
	for index, header := range headerRow {
		base := strings.TrimSpace(header)
		if base == "" {
			base = fmt.Sprintf("column_%d", index+1)
		}
		name := base
		if count := seen[base]; count > 0 {
			name = fmt.Sprintf("%s_%d", base, count+1)
		}
		seen[base]++
		headers = append(headers, name)
	}
	return headers
}

func csvColumnsFromRows(rows []any) []string {
	columns := make(map[string]struct{})
	for _, rawRow := range rows {
		row, ok := asMap(rawRow)
		if !ok {
			continue
		}
		for key := range row {
			columns[key] = struct{}{}
		}
	}

	names := make([]string, 0, len(columns))
	for key := range columns {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}

func builtinYAMLStringify(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("yaml_stringify", args, 1); err != nil {
		return nil, err
	}
	encoded, err := marshalYAMLValue(args[0])
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func parseYAMLText(text string) (any, error) {
	var value any
	if err := yaml.Unmarshal([]byte(text), &value); err != nil {
		return nil, err
	}
	return normalizeYAMLValue(value), nil
}

func marshalYAMLValue(value any) ([]byte, error) {
	encoded, err := yaml.Marshal(normalizeYAMLValue(value))
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func normalizeYAMLValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized[key] = normalizeYAMLValue(item)
		}
		return normalized
	case map[any]any:
		normalized := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized[stringify(key)] = normalizeYAMLValue(item)
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(typed))
		for _, item := range typed {
			normalized = append(normalized, normalizeYAMLValue(item))
		}
		return normalized
	default:
		return normalizeJSONValue(value)
	}
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

func builtinDictHas(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("dict_has", args, 2); err != nil {
		return nil, err
	}
	dict, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("dict_has expects a dict")
	}
	_, exists := dict[stringify(args[1])]
	return exists, nil
}

func builtinDictGet(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("dict_get", args, 3); err != nil {
		return nil, err
	}
	dict, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("dict_get expects a dict")
	}
	if value, exists := dict[stringify(args[1])]; exists {
		return value, nil
	}
	return args[2], nil
}

func builtinDictItems(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("dict_items", args, 1); err != nil {
		return nil, err
	}
	dict, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("dict_items expects a dict")
	}
	keys := make([]string, 0, len(dict))
	for key := range dict {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]any, 0, len(keys))
	for _, key := range keys {
		result = append(result, map[string]any{
			"key":   key,
			"value": cloneValue(dict[key]),
		})
	}
	return result, nil
}

func builtinDictSet(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("dict_set", args, 3); err != nil {
		return nil, err
	}
	dict, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("dict_set expects a dict")
	}
	result := cloneValue(dict).(map[string]any)
	result[stringify(args[1])] = args[2]
	return result, nil
}

func builtinDictMerge(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("dict_merge", args, 2); err != nil {
		return nil, err
	}
	left, ok := asMap(args[0])
	if !ok {
		return nil, fmt.Errorf("dict_merge expects left to be a dict")
	}
	right, ok := asMap(args[1])
	if !ok {
		return nil, fmt.Errorf("dict_merge expects right to be a dict")
	}
	result := cloneValue(left).(map[string]any)
	for key, value := range right {
		result[key] = cloneValue(value)
	}
	return result, nil
}

func builtinEnumerate(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("enumerate", args, 2); err != nil {
		return nil, err
	}
	values, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("enumerate expects values to be a list")
	}
	start, err := requireInt("enumerate", args[1], "start")
	if err != nil {
		return nil, err
	}

	result := make([]any, 0, len(values))
	for index, value := range values {
		result = append(result, map[string]any{
			"index": start + int64(index),
			"value": cloneValue(value),
		})
	}
	return result, nil
}

func builtinZip(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("zip", args, 3); err != nil {
		return nil, err
	}
	left, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("zip expects left to be a list")
	}
	right, ok := asList(args[1])
	if !ok {
		return nil, fmt.Errorf("zip expects right to be a list")
	}
	strict, err := coerceBool(args[2])
	if err != nil {
		return nil, fmt.Errorf("zip strict: %w", err)
	}
	if strict && len(left) != len(right) {
		return nil, fmt.Errorf("zip strict mode requires lists of equal length")
	}

	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}

	result := make([]any, 0, limit)
	for index := range limit {
		result = append(result, []any{cloneValue(left[index]), cloneValue(right[index])})
	}
	return result, nil
}

func builtinSorted(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("sorted", args, 2); err != nil {
		return nil, err
	}
	values, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("sorted expects a list")
	}
	descending, err := coerceBool(args[1])
	if err != nil {
		return nil, fmt.Errorf("sorted descending: %w", err)
	}
	result := make([]any, len(values))
	copy(result, values)
	sort.Slice(result, func(left, right int) bool {
		comparison := compareBuiltinValues(result[left], result[right])
		if descending {
			return comparison > 0
		}
		return comparison < 0
	})
	return result, nil
}

func builtinUnique(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("unique", args, 1); err != nil {
		return nil, err
	}
	values, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("unique expects a list")
	}
	result := make([]any, 0, len(values))
	for _, candidate := range values {
		seen := false
		for _, existing := range result {
			if reflect.DeepEqual(existing, candidate) {
				seen = true
				break
			}
		}
		if !seen {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func builtinSum(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("sum", args, 1); err != nil {
		return nil, err
	}
	values, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("sum expects a list")
	}
	if len(values) == 0 {
		return int64(0), nil
	}

	total := 0.0
	allInts := true
	for _, value := range values {
		number, ok := asFloat(value)
		if !ok {
			return nil, fmt.Errorf("sum expects only numeric values")
		}
		if _, isInt := asInt(value); !isInt {
			allInts = false
		}
		total += number
	}
	if allInts {
		return int64(total), nil
	}
	return total, nil
}

func builtinMin(_ context.Context, _ *Interpreter, args []any) (any, error) {
	return builtinExtrema("min", args, func(comparison int) bool { return comparison < 0 })
}

func builtinMax(_ context.Context, _ *Interpreter, args []any) (any, error) {
	return builtinExtrema("max", args, func(comparison int) bool { return comparison > 0 })
}

func builtinExtrema(name string, args []any, choose func(int) bool) (any, error) {
	if err := expectArgCount(name, args, 1); err != nil {
		return nil, err
	}
	values, ok := asList(args[0])
	if !ok {
		return nil, fmt.Errorf("%s expects a list", name)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s expects a non-empty list", name)
	}

	best := values[0]
	for _, candidate := range values[1:] {
		if choose(compareBuiltinValues(candidate, best)) {
			best = candidate
		}
	}
	return best, nil
}

func compareBuiltinValues(left, right any) int {
	if leftNumber, leftOK := asFloat(left); leftOK {
		if rightNumber, rightOK := asFloat(right); rightOK {
			switch {
			case leftNumber < rightNumber:
				return -1
			case leftNumber > rightNumber:
				return 1
			default:
				return 0
			}
		}
	}

	switch leftValue := left.(type) {
	case string:
		if rightValue, ok := right.(string); ok {
			switch {
			case leftValue < rightValue:
				return -1
			case leftValue > rightValue:
				return 1
			default:
				return 0
			}
		}
	case bool:
		if rightValue, ok := right.(bool); ok {
			switch {
			case !leftValue && rightValue:
				return -1
			case leftValue && !rightValue:
				return 1
			default:
				return 0
			}
		}
	}

	leftType := typeName(left)
	rightType := typeName(right)
	switch {
	case leftType < rightType:
		return -1
	case leftType > rightType:
		return 1
	}

	leftText := jsonString(normalizeJSONValue(left))
	rightText := jsonString(normalizeJSONValue(right))
	switch {
	case leftText < rightText:
		return -1
	case leftText > rightText:
		return 1
	default:
		return 0
	}
}
