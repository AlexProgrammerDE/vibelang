package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"vibelang/internal/ast"
)

func registerObservabilityBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, &builtinFunction{
		name: "log",
		call: builtinLog,
		tool: &ToolSpec{
			Name:       "log",
			ReturnType: ast.TypeRef{Expr: "none"},
			Body:       "Write one structured log record with a message, level, and optional fields.",
			Params: []ast.Param{
				{Name: "message", Type: ast.TypeRef{Expr: "string"}},
				{Name: "level", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"info\""},
				{Name: "fields", Type: ast.TypeRef{Expr: "dict"}, DefaultText: "{}"},
			},
		},
		defaults: map[string]any{
			"level":  "info",
			"fields": map[string]any{},
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "otel_init_stdout",
		call: builtinOTelInitStdout,
		tool: &ToolSpec{
			Name:       "otel_init_stdout",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "Configure OpenTelemetry tracing to write spans to stderr in stdout exporter format.",
			Params: []ast.Param{
				{Name: "service_name", Type: ast.TypeRef{Expr: "string"}, DefaultText: "\"vibelang\""},
				{Name: "pretty", Type: ast.TypeRef{Expr: "bool"}, DefaultText: "true"},
			},
		},
		defaults: map[string]any{
			"service_name": "vibelang",
			"pretty":       true,
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "otel_span_start",
		call: builtinOTelSpanStart,
		tool: &ToolSpec{
			Name:       "otel_span_start",
			ReturnType: ast.TypeRef{Expr: "string"},
			Body:       "Start an OpenTelemetry span and return its handle.",
			Params: []ast.Param{
				{Name: "name", Type: ast.TypeRef{Expr: "string"}},
				{Name: "attributes", Type: ast.TypeRef{Expr: "dict"}, DefaultText: "{}"},
			},
		},
		defaults: map[string]any{
			"attributes": map[string]any{},
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "otel_span_event",
		call: builtinOTelSpanEvent,
		tool: &ToolSpec{
			Name:       "otel_span_event",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "Add an event to an OpenTelemetry span handle.",
			Params: []ast.Param{
				{Name: "span", Type: ast.TypeRef{Expr: "string"}},
				{Name: "name", Type: ast.TypeRef{Expr: "string"}},
				{Name: "attributes", Type: ast.TypeRef{Expr: "dict"}, DefaultText: "{}"},
			},
		},
		defaults: map[string]any{
			"attributes": map[string]any{},
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, &builtinFunction{
		name: "otel_span_end",
		call: builtinOTelSpanEnd,
		tool: &ToolSpec{
			Name:       "otel_span_end",
			ReturnType: ast.TypeRef{Expr: "bool"},
			Body:       "End an OpenTelemetry span handle.",
			Params: []ast.Param{
				{Name: "span", Type: ast.TypeRef{Expr: "string"}},
				{Name: "attributes", Type: ast.TypeRef{Expr: "dict"}, DefaultText: "{}"},
			},
		},
		defaults: map[string]any{
			"attributes": map[string]any{},
		},
		bindArgs: true,
	})
	registerBuiltin(interpreter, toolBuiltin("otel_flush", builtinOTelFlush, "bool", "Flush pending OpenTelemetry exports."))
}

func builtinLog(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("log", args, 3); err != nil {
		return nil, err
	}

	message, err := requireString("log", args[0], "message")
	if err != nil {
		return nil, err
	}
	level, err := requireString("log", args[1], "level")
	if err != nil {
		return nil, err
	}
	fields, ok := asMap(args[2])
	if !ok {
		return nil, fmt.Errorf("log expects fields to be a dict")
	}

	record := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     level,
		"message":   message,
		"fields":    normalizeJSONValue(fields),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if _, err := interpreter.stderr.Write(append(encoded, '\n')); err != nil {
		return nil, err
	}
	interpreter.incrementMetric("logs_emitted_total", 1)
	return nil, nil
}

func builtinOTelInitStdout(ctx context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("otel_init_stdout", args, 2); err != nil {
		return nil, err
	}
	serviceName, err := requireString("otel_init_stdout", args[0], "service_name")
	if err != nil {
		return nil, err
	}
	pretty, err := coerceBool(args[1])
	if err != nil {
		return nil, err
	}
	if err := interpreter.telemetry.ConfigureStdout(ctx, serviceName, pretty); err != nil {
		return nil, err
	}
	return true, nil
}

func builtinOTelSpanStart(ctx context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("otel_span_start", args, 2); err != nil {
		return nil, err
	}
	name, err := requireString("otel_span_start", args[0], "name")
	if err != nil {
		return nil, err
	}
	attributes, ok := asMap(args[1])
	if !ok {
		return nil, fmt.Errorf("otel_span_start expects attributes to be a dict")
	}
	if err := interpreter.telemetry.ensure(ctx); err != nil {
		return nil, err
	}
	handleID := interpreter.nextHandle("otel_span")
	if err := interpreter.telemetry.StartSpan(ctx, handleID, name, attributes); err != nil {
		return nil, err
	}
	return handleID, nil
}

func builtinOTelSpanEvent(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("otel_span_event", args, 3); err != nil {
		return nil, err
	}
	handleID, err := requireString("otel_span_event", args[0], "span")
	if err != nil {
		return nil, err
	}
	name, err := requireString("otel_span_event", args[1], "name")
	if err != nil {
		return nil, err
	}
	attributes, ok := asMap(args[2])
	if !ok {
		return nil, fmt.Errorf("otel_span_event expects attributes to be a dict")
	}
	if err := interpreter.telemetry.AddEvent(handleID, name, attributes); err != nil {
		return nil, err
	}
	return true, nil
}

func builtinOTelSpanEnd(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("otel_span_end", args, 2); err != nil {
		return nil, err
	}
	handleID, err := requireString("otel_span_end", args[0], "span")
	if err != nil {
		return nil, err
	}
	attributes, ok := asMap(args[1])
	if !ok {
		return nil, fmt.Errorf("otel_span_end expects attributes to be a dict")
	}
	return interpreter.telemetry.EndSpan(handleID, attributes)
}

func builtinOTelFlush(ctx context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("otel_flush", args, 0); err != nil {
		return nil, err
	}
	if err := interpreter.telemetry.Flush(ctx); err != nil {
		return nil, err
	}
	return true, nil
}
