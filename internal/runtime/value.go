package runtime

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return "none"
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any, map[string]any:
		return jsonString(v)
	case Callable:
		return fmt.Sprintf("<function %s>", v.Name())
	case MacroCallable:
		return fmt.Sprintf("<macro %s>", v.Name())
	case withContext:
		return fmt.Sprintf("<context %s>", v.Name())
	case *SetValue:
		return fmt.Sprintf("set(%s)", jsonString(v.Values()))
	default:
		return fmt.Sprint(v)
	}
}

func jsonString(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func typeName(value any) string {
	switch value.(type) {
	case nil:
		return "none"
	case string:
		return "string"
	case bool:
		return "bool"
	case int, int64:
		return "int"
	case float64:
		return "float"
	case []any:
		return "list"
	case map[string]any:
		return "dict"
	case Callable:
		return "function"
	case MacroCallable:
		return "macro"
	case withContext:
		return "context"
	case *SetValue:
		return "set"
	default:
		return reflect.TypeOf(value).String()
	}
}

func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	case *SetValue:
		return v.Len() > 0
	default:
		return true
	}
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func asInt(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		if math.Trunc(v) != v {
			return 0, false
		}
		return int64(v), true
	default:
		return 0, false
	}
}

func asList(value any) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, true
	case []string:
		result := make([]any, 0, len(v))
		for _, item := range v {
			result = append(result, item)
		}
		return result, true
	default:
		return nil, false
	}
}

func asMap(value any) (map[string]any, bool) {
	v, ok := value.(map[string]any)
	return v, ok
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case []any:
		cloned := make([]any, len(v))
		for index, item := range v {
			cloned[index] = cloneValue(item)
		}
		return cloned
	case map[string]any:
		cloned := make(map[string]any, len(v))
		for key, item := range v {
			cloned[key] = cloneValue(item)
		}
		return cloned
	case *SetValue:
		return v.Clone()
	default:
		return value
	}
}

func iterableValues(value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return v, nil
	case string:
		result := make([]any, 0, len(v))
		for _, ch := range v {
			result = append(result, string(ch))
		}
		return result, nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make([]any, 0, len(keys))
		for _, key := range keys {
			result = append(result, key)
		}
		return result, nil
	case *SetValue:
		return v.Values(), nil
	default:
		return nil, fmt.Errorf("value of type %s is not iterable", typeName(value))
	}
}

func containsValue(container, needle any) (bool, error) {
	switch v := container.(type) {
	case string:
		return containsString(v, stringify(needle)), nil
	case []any:
		for _, item := range v {
			if reflect.DeepEqual(item, needle) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		_, ok := v[stringify(needle)]
		return ok, nil
	case *SetValue:
		return v.Has(needle), nil
	default:
		return false, fmt.Errorf("operator 'in' does not support %s", typeName(container))
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func normalizeSequenceIndex(index any, length int, kind string) (int, error) {
	position, ok := asInt(index)
	if !ok {
		return 0, fmt.Errorf("%s index must be an integer", kind)
	}
	if position < 0 {
		position = int64(length) + position
	}
	if position < 0 || int(position) >= length {
		return 0, fmt.Errorf("%s index %d out of range", kind, position)
	}
	return int(position), nil
}

func normalizeSliceBounds(length int, startValue, endValue, stepValue any) (int, int, int, error) {
	step := int64(1)
	if stepValue != nil {
		parsedStep, ok := asInt(stepValue)
		if !ok {
			return 0, 0, 0, fmt.Errorf("slice step must be an integer")
		}
		if parsedStep == 0 {
			return 0, 0, 0, fmt.Errorf("slice step cannot be zero")
		}
		step = parsedStep
	}

	if step > 0 {
		start, err := normalizeSliceBound(startValue, length, 0, 0, length)
		if err != nil {
			return 0, 0, 0, err
		}
		end, err := normalizeSliceBound(endValue, length, length, 0, length)
		if err != nil {
			return 0, 0, 0, err
		}
		return start, end, int(step), nil
	}

	start, err := normalizeSliceBound(startValue, length, length-1, -1, length-1)
	if err != nil {
		return 0, 0, 0, err
	}
	end, err := normalizeSliceBound(endValue, length, -1, -1, length-1)
	if err != nil {
		return 0, 0, 0, err
	}
	return start, end, int(step), nil
}

func normalizeSliceBound(value any, length int, defaultValue, minValue, maxValue int) (int, error) {
	if value == nil {
		return defaultValue, nil
	}

	index, ok := asInt(value)
	if !ok {
		return 0, fmt.Errorf("slice bounds must be integers")
	}
	if index < 0 {
		index += int64(length)
	}

	position := int(index)
	if position < minValue {
		position = minValue
	}
	if position > maxValue {
		position = maxValue
	}
	return position, nil
}
