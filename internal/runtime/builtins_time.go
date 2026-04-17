package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vibelang/internal/ast"
)

func registerTimeBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, &builtinFunction{
		name: "time_parse",
		call: builtinTimeParse,
		tool: &ToolSpec{
			Name:       "time_parse",
			ReturnType: ast.TypeRef{Expr: "dict"},
			Body:       "Parse a time string using a named layout like rfc3339, rfc3339_nano, date, datetime, or time, or a raw Go time layout. Return a dict with calendar fields and Unix timestamps.",
			Params: []ast.Param{
				{Name: "text", Type: ast.TypeRef{Expr: "string"}},
				{Name: "layout", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"rfc3339\""},
			},
		},
		defaults: map[string]any{
			"layout": "rfc3339",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "time_format",
		call: builtinTimeFormat,
		tool: &ToolSpec{
			Name:       "time_format",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Format a time value. Accept either a time string or a dict returned by time_parse.",
			Params: []ast.Param{
				{Name: "value"},
				{Name: "layout", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"rfc3339\""},
				{Name: "input_layout", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"rfc3339\""},
			},
		},
		defaults: map[string]any{
			"layout":       "rfc3339",
			"input_layout": "rfc3339",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "time_add",
		call: builtinTimeAdd,
		tool: &ToolSpec{
			Name:       "time_add",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Add a Go duration string like 90m or 2h45m to a time value and return the resulting RFC3339 timestamp.",
			Params: []ast.Param{
				{Name: "value"},
				{Name: "duration", Type: ast.TypeRef{Expr: "string"}},
				{Name: "input_layout", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"rfc3339\""},
			},
		},
		defaults: map[string]any{
			"input_layout": "rfc3339",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "time_diff",
		call: builtinTimeDiff,
		tool: &ToolSpec{
			Name:       "time_diff",
			ReturnType: ast.TypeRef{Expr: "int"},
			Body:       "Return the difference end-start in the requested unit: nanoseconds, microseconds, milliseconds, seconds, minutes, or hours.",
			Params: []ast.Param{
				{Name: "start"},
				{Name: "end"},
				{Name: "unit", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"milliseconds\""},
				{Name: "input_layout", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"rfc3339\""},
			},
		},
		defaults: map[string]any{
			"unit":         "milliseconds",
			"input_layout": "rfc3339",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "duration_parse",
		call: builtinDurationParse,
		tool: &ToolSpec{
			Name:       "duration_parse",
			ReturnType: ast.TypeRef{Expr: "int"},
			Body:       "Parse a Go duration string and return it in the requested unit: nanoseconds, microseconds, milliseconds, seconds, minutes, or hours.",
			Params: []ast.Param{
				{Name: "text", Type: ast.TypeRef{Expr: "string"}},
				{Name: "unit", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"milliseconds\""},
			},
		},
		defaults: map[string]any{
			"unit": "milliseconds",
		},
		bindArgs:   true,
		promptSafe: true,
	})
	registerBuiltin(interpreter, promptToolBuiltin("uuid_v4", builtinUUIDv4, "string", "Generate a random UUID version 4 string."))
	registerBuiltin(interpreter, promptToolBuiltin("uuid_v7", builtinUUIDv7, "string", "Generate a time-ordered UUID version 7 string."))
}

func builtinTimeParse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("time_parse", args, 2); err != nil {
		return nil, err
	}
	text, err := requireString("time_parse", args[0], "text")
	if err != nil {
		return nil, err
	}
	layoutName, err := requireString("time_parse", args[1], "layout")
	if err != nil {
		return nil, err
	}
	layout, err := resolveTimeLayout(layoutName)
	if err != nil {
		return nil, err
	}

	parsed, err := time.Parse(layout, text)
	if err != nil {
		return nil, err
	}
	zoneName, zoneOffset := parsed.Zone()
	return map[string]any{
		"year":           int64(parsed.Year()),
		"month":          int64(parsed.Month()),
		"day":            int64(parsed.Day()),
		"hour":           int64(parsed.Hour()),
		"minute":         int64(parsed.Minute()),
		"second":         int64(parsed.Second()),
		"nanosecond":     int64(parsed.Nanosecond()),
		"weekday":        strings.ToLower(parsed.Weekday().String()),
		"yearday":        int64(parsed.YearDay()),
		"timezone":       zoneName,
		"offset_seconds": int64(zoneOffset),
		"unix":           parsed.Unix(),
		"unix_ms":        parsed.UnixMilli(),
		"unix_nano":      parsed.UnixNano(),
		"rfc3339":        parsed.Format(time.RFC3339Nano),
		"date":           parsed.Format("2006-01-02"),
		"time":           parsed.Format("15:04:05"),
	}, nil
}

func builtinTimeFormat(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("time_format", args, 3); err != nil {
		return nil, err
	}
	outputLayoutName, err := requireString("time_format", args[1], "layout")
	if err != nil {
		return nil, err
	}
	outputLayout, err := resolveTimeLayout(outputLayoutName)
	if err != nil {
		return nil, err
	}
	inputLayoutName, err := requireString("time_format", args[2], "input_layout")
	if err != nil {
		return nil, err
	}

	parsed, err := timeFromValue("time_format", args[0], "value", inputLayoutName)
	if err != nil {
		return nil, err
	}
	return parsed.Format(outputLayout), nil
}

func builtinTimeAdd(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("time_add", args, 3); err != nil {
		return nil, err
	}
	durationText, err := requireString("time_add", args[1], "duration")
	if err != nil {
		return nil, err
	}
	durationValue, err := time.ParseDuration(durationText)
	if err != nil {
		return nil, err
	}
	inputLayoutName, err := requireString("time_add", args[2], "input_layout")
	if err != nil {
		return nil, err
	}

	parsed, err := timeFromValue("time_add", args[0], "value", inputLayoutName)
	if err != nil {
		return nil, err
	}
	return parsed.Add(durationValue).Format(time.RFC3339Nano), nil
}

func builtinTimeDiff(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("time_diff", args, 4); err != nil {
		return nil, err
	}
	unit, err := requireString("time_diff", args[2], "unit")
	if err != nil {
		return nil, err
	}
	inputLayoutName, err := requireString("time_diff", args[3], "input_layout")
	if err != nil {
		return nil, err
	}

	start, err := timeFromValue("time_diff", args[0], "start", inputLayoutName)
	if err != nil {
		return nil, err
	}
	end, err := timeFromValue("time_diff", args[1], "end", inputLayoutName)
	if err != nil {
		return nil, err
	}
	return durationInUnit(end.Sub(start), unit)
}

func builtinDurationParse(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("duration_parse", args, 2); err != nil {
		return nil, err
	}
	text, err := requireString("duration_parse", args[0], "text")
	if err != nil {
		return nil, err
	}
	unit, err := requireString("duration_parse", args[1], "unit")
	if err != nil {
		return nil, err
	}

	durationValue, err := time.ParseDuration(text)
	if err != nil {
		return nil, err
	}
	return durationInUnit(durationValue, unit)
}

func builtinUUIDv4(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("uuid_v4", args, 0); err != nil {
		return nil, err
	}
	value, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	return value.String(), nil
}

func builtinUUIDv7(_ context.Context, _ *Interpreter, args []any) (any, error) {
	if err := expectArgCount("uuid_v7", args, 0); err != nil {
		return nil, err
	}
	value, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return value.String(), nil
}

func resolveTimeLayout(layout string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(layout)) {
	case "", "rfc3339":
		return time.RFC3339, nil
	case "rfc3339_nano":
		return time.RFC3339Nano, nil
	case "date":
		return "2006-01-02", nil
	case "datetime":
		return "2006-01-02T15:04:05", nil
	case "time":
		return "15:04:05", nil
	default:
		if strings.TrimSpace(layout) == "" {
			return "", fmt.Errorf("time layout cannot be empty")
		}
		return layout, nil
	}
}

func timeFromValue(name string, value any, param, inputLayoutName string) (time.Time, error) {
	switch typed := value.(type) {
	case string:
		layout, err := resolveTimeLayout(inputLayoutName)
		if err != nil {
			return time.Time{}, err
		}
		return time.Parse(layout, typed)
	case map[string]any:
		raw, ok := typed["rfc3339"].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			return time.Time{}, fmt.Errorf("%s expects %s dict values to contain a non-empty rfc3339 field", name, param)
		}
		return time.Parse(time.RFC3339Nano, raw)
	default:
		return time.Time{}, fmt.Errorf("%s expects %s to be a string or a dict from time_parse", name, param)
	}
}

func durationInUnit(value time.Duration, unit string) (int64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "milliseconds", "millisecond", "ms":
		return value.Milliseconds(), nil
	case "nanoseconds", "nanosecond", "ns":
		return value.Nanoseconds(), nil
	case "microseconds", "microsecond", "us", "µs":
		return value.Microseconds(), nil
	case "seconds", "second", "s":
		return int64(value / time.Second), nil
	case "minutes", "minute", "m":
		return int64(value / time.Minute), nil
	case "hours", "hour", "h":
		return int64(value / time.Hour), nil
	default:
		return 0, fmt.Errorf("unsupported duration unit %q", unit)
	}
}
