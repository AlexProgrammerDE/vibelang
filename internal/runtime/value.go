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
	default:
		return false, fmt.Errorf("operator 'in' does not support %s", typeName(container))
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
