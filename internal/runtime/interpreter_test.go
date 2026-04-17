package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vibelang/internal/model"
	"vibelang/internal/parser"
)

type scriptedClient struct {
	responses []string
	prompts   []string
	index     int
}

func (c *scriptedClient) Generate(_ context.Context, request model.Request) (model.Response, error) {
	c.prompts = append(c.prompts, request.Prompt)
	if c.index >= len(c.responses) {
		return model.Response{}, errors.New("unexpected model call")
	}
	response := c.responses[c.index]
	c.index++
	return model.Response{Text: response}, nil
}

func TestInterpreterExecutesControlFlow(t *testing.T) {
	source := `numbers = range(1, 5)
total = 0
for value in numbers:
    total = total + value
print(total)
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

	if stdout.String() != "10\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "10\n", stdout.String())
	}
}

func TestInterpreterRunsAIFunction(t *testing.T) {
	source := `def add_one(value: int) -> int:
    Add one to the input and return the integer result.

result = add_one(41)
print(result)
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"42"}`,
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

	if len(client.prompts) != 1 {
		t.Fatalf("expected 1 model prompt, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "add_one(value: int) -> int") {
		t.Fatalf("prompt did not include function signature:\n%s", client.prompts[0])
	}
}

func TestInterpreterSupportsAIToolCalls(t *testing.T) {
	source := `def double(value: int) -> int:
    Multiply the input by two and return the integer result.

def describe(value: int) -> string:
    Call double to get twice the input, then describe the result.

message = describe(21)
print(message)
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"double","arguments":{"value":21}}}`,
			`{"action":"return","value":42}`,
			`{"action":"return","value":"double is 42"}`,
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

	if stdout.String() != "double is 42\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "double is 42\n", stdout.String())
	}

	if len(client.prompts) != 3 {
		t.Fatalf("expected 3 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[2], "Tool history:") {
		t.Fatalf("final prompt did not include tool history:\n%s", client.prompts[2])
	}
}

func TestInterpreterSupportsInlinePromptExpressions(t *testing.T) {
	source := `def double(value: int) -> int:
    Multiply the input by two and return the integer result.

result = * call double with 21 and return the integer result.
print(result)
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"double","arguments":{"value":21}}}`,
			`{"action":"return","value":42}`,
			`{"action":"return","value":42}`,
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

	if len(client.prompts) != 3 {
		t.Fatalf("expected 3 model prompts, got %d", len(client.prompts))
	}
}

func TestInterpreterInlinePromptsCanUseFilesystemTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi.txt")
	source := fmt.Sprintf(`path = %q
digits = * return the first 5 digits of pi as a string.
if * check whether ${path} exists:
    * delete the file at ${path}.
else:
    * write ${digits} to the file at ${path}.
print(file_exists(path))
print(read_file(path))
`, path)

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"31415"}`,
			fmt.Sprintf(`{"action":"call","call":{"name":"file_exists","arguments":{"path":%q}}}`, path),
			`{"action":"return","value":false}`,
			fmt.Sprintf(`{"action":"call","call":{"name":"write_file","arguments":{"path":%q,"content":"31415"}}}`, path),
			`{"action":"return","value":"done"}`,
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

	if stdout.String() != "true\n31415\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "true\n31415\n", stdout.String())
	}
}

func TestInterpreterInterpolatesExpressionsInAIFunctionBodies(t *testing.T) {
	source := `def describe(items: list[string]) -> string:
    The first item is ${items[0]}.
    The list length is ${len(items)}.

values = ["alpha", "beta", "gamma"]
print(describe(values))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"ok"}`,
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

	if len(client.prompts) != 1 {
		t.Fatalf("expected 1 model prompt, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "The first item is alpha.") {
		t.Fatalf("prompt did not interpolate indexed value:\n%s", client.prompts[0])
	}
	if !strings.Contains(client.prompts[0], "The list length is 3.") {
		t.Fatalf("prompt did not interpolate len() result:\n%s", client.prompts[0])
	}
}

func TestInterpreterInterpolatesExpressionsInInlinePrompts(t *testing.T) {
	source := `items = ["alpha", "beta"]
message = * mention ${items[1]} and the length ${len(items)}.
print(message)
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"ok"}`,
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

	if len(client.prompts) != 1 {
		t.Fatalf("expected 1 model prompt, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "mention beta and the length 2.") {
		t.Fatalf("inline prompt did not interpolate expressions:\n%s", client.prompts[0])
	}
}

func TestInterpreterProvidesExpandedStandardLibrary(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer os.Chdir(originalWD)

	t.Setenv("VIBELANG_TEST_ENV", "configured")

	source := `workspace = join_path([cwd(), "data"])
make_dir(workspace)
text_path = join_path([workspace, "notes.txt"])
json_path = join_path([workspace, "config.json"])
write_file(text_path, trim("  hello  "))
append_file(text_path, "\nworld")
write_json(json_path, {"name": upper("ada"), "count": len(split("a,b,c", ","))})
payload = read_json(json_path)
entries = list_dir(workspace)
print(basename(text_path))
print(dirname(text_path) == workspace)
print(abs_path("data") == workspace)
print(is_dir(workspace))
print(contains(entries, "notes.txt"))
print(payload["name"])
print(payload["count"])
print(join(split("a,b,c", ","), "-"))
print(lower(replace("HELLO ADA", "ADA", env("VIBELANG_TEST_ENV"))))
print(read_file(text_path))
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

	want := strings.Join([]string{
		"notes.txt",
		"true",
		"true",
		"true",
		"true",
		"ADA",
		"3",
		"a-b-c",
		"hello configured",
		"hello",
		"world",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}
