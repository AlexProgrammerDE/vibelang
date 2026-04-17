package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibelang/internal/parser"
)

func TestInterpreterSupportsAIMacros(t *testing.T) {
	source := `macro double_expr(value: int) -> int:
    Return the vibelang expression that doubles ${value}.

result = @double_expr(21)
print(result)
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"expand","source":"21 + 21"}`,
		},
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{
		Model:  client,
		Stdout: &stdout,
	})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if stdout.String() != "42\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "42\n", stdout.String())
	}
}

func TestInterpreterImportsMacrosFromModules(t *testing.T) {
	tempDir := t.TempDir()
	modulePath := filepath.Join(tempDir, "shared.vibe")
	moduleSource := `macro even_numbers(limit: int) -> list[int]:
    Return one valid vibelang expression that builds the even numbers below ${limit} * 2.
`
	if err := os.WriteFile(modulePath, []byte(moduleSource), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := `import "./shared.vibe" as shared
print(json(@shared.even_numbers(4)))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"expand","source":"range(start=0, stop=8, step=2)"}`,
		},
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer os.Chdir(originalWD)

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{
		Model:  client,
		Stdout: &stdout,
	})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if stdout.String() != "[0,2,4,6]\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "[0,2,4,6]\n", stdout.String())
	}
}

func TestInterpreterProvidesSetsAndJSONStringHelpers(t *testing.T) {
	source := `items = set(["alpha", "beta", "alpha"])
items = set_add(items, "gamma")
payload = json_parse("{\"items\":[1,2,3],\"ok\":true}")
pretty = json_pretty(payload)

print(len(items))
print(set_has(items, "beta"))
print(json(set_values(set_intersection(items, set(["beta", "delta"])))))
print(payload["items"][1])
print(contains(pretty, "\n"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := "3\ntrue\n[\"beta\"]\n2\ntrue\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterProvidesStructuredLogging(t *testing.T) {
	source := `log("booted", fields={"service": "demo", "port": 8080})`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stderr bytes.Buffer
	interpreter := NewInterpreter(Config{Stderr: &stderr})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, `"message":"booted"`) {
		t.Fatalf("expected log output to include the message, got %q", output)
	}
	if !strings.Contains(output, `"service":"demo"`) {
		t.Fatalf("expected log output to include fields, got %q", output)
	}
}

func TestInterpreterProvidesOpenTelemetryTracing(t *testing.T) {
	source := `otel_init_stdout(service_name="vibelang-test")
span = otel_span_start("boot", {"phase": "init"})
otel_span_event(span, "ready", {"ok": true})
otel_span_end(span, {"status": "ok"})
otel_flush()
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stderr bytes.Buffer
	interpreter := NewInterpreter(Config{Stderr: &stderr})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "boot") {
		t.Fatalf("expected trace output to include the span name, got %q", output)
	}
	if !strings.Contains(output, "ready") {
		t.Fatalf("expected trace output to include the span event, got %q", output)
	}
}
