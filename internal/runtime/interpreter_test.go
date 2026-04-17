package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestInterpreterSupportsDefaultParametersAndKeywordCalls(t *testing.T) {
	source := `def summarize(name: string, tone: string = "dry") -> string:
    Return exactly: ${tone} summary for ${name}.

print(summarize("Ada"))
print(summarize(name="Ada", tone="playful"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"dry summary for Ada"}`,
			`{"action":"return","value":"playful summary for Ada"}`,
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

	if stdout.String() != "dry summary for Ada\nplayful summary for Ada\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "dry summary for Ada\nplayful summary for Ada\n", stdout.String())
	}

	if len(client.prompts) != 2 {
		t.Fatalf("expected 2 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "Return exactly: dry summary for Ada.") {
		t.Fatalf("first prompt did not use the default parameter:\n%s", client.prompts[0])
	}
	if !strings.Contains(client.prompts[1], "Return exactly: playful summary for Ada.") {
		t.Fatalf("second prompt did not use keyword arguments:\n%s", client.prompts[1])
	}
}

func TestInterpreterAIHelpersCanUseDefaultParameters(t *testing.T) {
	source := `def format_name(name: string, prefix: string = "Dr.") -> string:
    Return exactly: ${prefix} ${name}

def describe(name: string) -> string:
    Call format_name with the provided name and return its output.

print(describe("Ada"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"format_name","arguments":{"name":"Ada"}}}`,
			`{"action":"return","value":"Dr. Ada"}`,
			`{"action":"return","value":"Dr. Ada"}`,
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

	if stdout.String() != "Dr. Ada\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "Dr. Ada\n", stdout.String())
	}

	if len(client.prompts) != 3 {
		t.Fatalf("expected 3 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[1], "Return exactly: Dr. Ada") {
		t.Fatalf("helper prompt did not use the default parameter:\n%s", client.prompts[1])
	}
}

func TestInterpreterBuiltinKeywordCallsUseDefaults(t *testing.T) {
	source := `print(range(stop=5))
print(range(start=2, stop=7))
print(range(start=10, stop=4, step=-2))
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

	want := "[0,1,2,3,4]\n[2,3,4,5,6]\n[10,8,6]\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterAIHelpersCanCallRange(t *testing.T) {
	source := `def make_numbers(stop: int) -> list[int]:
    Call range to produce the numbers from zero up to ${stop}.

print(json(make_numbers(4)))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"range","arguments":{"stop":4}}}`,
			`{"action":"return","value":[0,1,2,3]}`,
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

	if stdout.String() != "[0,1,2,3]\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "[0,1,2,3]\n", stdout.String())
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

func TestInterpreterImportsModulesFromFiles(t *testing.T) {
	tempDir := t.TempDir()
	modulePath := filepath.Join(tempDir, "shared.vibe")
	moduleSource := `prefix = "Dr."
def format_name(name: string) -> string:
    Return exactly: ${prefix} ${name}
`
	if err := os.WriteFile(modulePath, []byte(moduleSource), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := `from "./shared.vibe" import prefix, format_name
import "./shared.vibe" as shared
print(prefix)
print(format_name("Ada"))
print(shared["prefix"])
print(shared["format_name"]("Ada"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"Dr. Ada"}`,
			`{"action":"return","value":"Dr. Ada"}`,
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

	want := "Dr.\nDr. Ada\nDr.\nDr. Ada\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterProvidesNetworkAndSystemStdlib(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Test") != "vibelang" {
			http.Error(writer, "missing header", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(writer, "hello %s", request.URL.Query().Get("name"))
	}))
	defer server.Close()

	tempDir := t.TempDir()
	source := fmt.Sprintf(`workspace = join_path([cwd(), "network"])
make_dir(workspace)
src = join_path([workspace, "source.txt"])
copy = join_path([workspace, "copy.txt"])
moved = join_path([workspace, "moved.txt"])
write_file(src, "alpha")
copy_file(src, copy)
move_file(copy, moved)
matches = glob(join_path([workspace, "*.txt"]))
response = http_request(%q, headers={"X-Test": "vibelang"})
process = run_process("bash", args=["-lc", "printf 'hello %%s' \"$TARGET\""], env={"TARGET": "vibe"}, dir=workspace)
print(response["status"])
print(response["body"])
print(contains(matches, src))
print(contains(matches, moved))
print(read_file(moved))
print(sqrt(81))
print(pow(2, 5))
print(abs(-4))
print(floor(2.8))
print(ceil(2.2))
print(type(now()))
print(type(unix_time()))
print(process["success"])
print(process["stdout"])
`, server.URL+"?name=world")

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
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
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := strings.Join([]string{
		"200",
		"hello world",
		"true",
		"true",
		"alpha",
		"9",
		"32",
		"4",
		"2",
		"3",
		"string",
		"int",
		"true",
		"hello vibe",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterProvidesSocketStdlib(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		buffer := make([]byte, 16)
		n, err := conn.Read(buffer)
		if err != nil {
			done <- err
			return
		}
		if string(buffer[:n]) != "ping" {
			done <- fmt.Errorf("unexpected request %q", string(buffer[:n]))
			return
		}
		_, err = io.WriteString(conn, "pong")
		done <- err
	}()

	source := fmt.Sprintf(`handle = socket_open(%q)
socket_write(handle, "ping")
print(socket_read(handle))
print(socket_close(handle))
`, listener.Addr().String())

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("server returned error: %v", err)
	}

	want := "pong\ntrue\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}
