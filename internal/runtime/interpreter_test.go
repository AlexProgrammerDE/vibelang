package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
