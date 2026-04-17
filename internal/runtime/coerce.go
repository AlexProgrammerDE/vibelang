package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func Coerce(typeExpr string, value any) (any, error) {
	spec, err := parseTypeSpec(typeExpr)
	if err != nil {
		return nil, err
	}
	return spec.coerceValue(value)
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
