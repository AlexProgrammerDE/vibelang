package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func Coerce(typeExpr string, value any) (any, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(typeExpr), " ", ""))
	if normalized == "" || normalized == "any" {
		return value, nil
	}

	base, args, hasGeneric := parseGeneric(normalized)
	if !hasGeneric {
		switch normalized {
		case "string":
			return stringify(value), nil
		case "int":
			return coerceInt(value)
		case "float":
			return coerceFloat(value)
		case "bool":
			return coerceBool(value)
		case "none":
			if value != nil {
				return nil, fmt.Errorf("expected none, got %s", typeName(value))
			}
			return nil, nil
		case "list":
			if list, ok := asList(value); ok {
				return list, nil
			}
			return nil, fmt.Errorf("expected list, got %s", typeName(value))
		case "dict":
			if dict, ok := asMap(value); ok {
				return dict, nil
			}
			return nil, fmt.Errorf("expected dict, got %s", typeName(value))
		default:
			return value, nil
		}
	}

	switch base {
	case "list":
		list, ok := asList(value)
		if !ok {
			return nil, fmt.Errorf("expected list, got %s", typeName(value))
		}
		result := make([]any, 0, len(list))
		itemType := "any"
		if len(args) > 0 {
			itemType = args[0]
		}
		for _, item := range list {
			coerced, err := Coerce(itemType, item)
			if err != nil {
				return nil, err
			}
			result = append(result, coerced)
		}
		return result, nil
	case "dict":
		dict, ok := asMap(value)
		if !ok {
			return nil, fmt.Errorf("expected dict, got %s", typeName(value))
		}
		valueType := "any"
		if len(args) == 2 {
			valueType = args[1]
		} else if len(args) == 1 {
			valueType = args[0]
		}
		result := make(map[string]any, len(dict))
		for key, item := range dict {
			coerced, err := Coerce(valueType, item)
			if err != nil {
				return nil, err
			}
			result[key] = coerced
		}
		return result, nil
	default:
		return value, nil
	}
}

func coerceInt(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("cannot coerce non-integral float %v to int", v)
		}
		return int64(v), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot coerce %q to int", v)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("cannot coerce %s to int", typeName(value))
	}
}

func coerceFloat(value any) (float64, error) {
	switch v := value.(type) {
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("cannot coerce %q to float", v)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("cannot coerce %s to float", typeName(value))
	}
}

func coerceBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "1":
			return true, nil
		case "false", "no", "0":
			return false, nil
		default:
			return false, fmt.Errorf("cannot coerce %q to bool", v)
		}
	case int:
		return v != 0, nil
	case int64:
		return v != 0, nil
	case float64:
		return v != 0, nil
	default:
		return false, fmt.Errorf("cannot coerce %s to bool", typeName(value))
	}
}

func parseGeneric(typeExpr string) (string, []string, bool) {
	open := strings.IndexRune(typeExpr, '[')
	close := strings.LastIndex(typeExpr, "]")
	if open < 0 || close < open {
		return "", nil, false
	}

	base := typeExpr[:open]
	body := typeExpr[open+1 : close]
	parts := splitTopLevel(body)
	return base, parts, true
}

func splitTopLevel(text string) []string {
	parts := make([]string, 0)
	start := 0
	depth := 0
	for i, ch := range text {
		switch ch {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(text[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(text[start:]))
	return parts
}
