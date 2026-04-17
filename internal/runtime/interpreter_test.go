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
	"regexp"
	"strings"
	"testing"
	"time"

	"vibelang/internal/model"
	"vibelang/internal/parser"
)

type scriptedClient struct {
	responses []string
	queued    []model.Response
	prompts   []string
	requests  []model.Request
	index     int
}

func (c *scriptedClient) Generate(_ context.Context, request model.Request) (model.Response, error) {
	c.prompts = append(c.prompts, request.Prompt)
	c.requests = append(c.requests, request)
	if c.index < len(c.queued) {
		response := c.queued[c.index]
		c.index++
		return response, nil
	}
	if c.index >= len(c.responses) {
		return model.Response{}, errors.New("unexpected model call")
	}
	response := c.responses[c.index]
	c.index++
	return model.Response{Text: response}, nil
}

func nestedMap(root map[string]any, keys ...string) (map[string]any, bool) {
	current := root
	for _, key := range keys {
		value, ok := current[key]
		if !ok {
			return nil, false
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
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

func TestInterpreterSupportsMatchStatements(t *testing.T) {
	source := `packet = {"type": "message", "payload": ["alpha", "beta"], "meta": {"city": "Berlin"}}

match packet:
    case {"type": "ping"}:
        print("pong")
    case {"type": "message", "payload": [head, tail], "meta": {"city": city}}:
        print(head)
        print(tail)
        print(city)
    case _:
        print("fallback")
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

	want := "alpha\nbeta\nBerlin\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsMatchGuards(t *testing.T) {
	source := `packet = {"type": "message", "payload": ["alpha", "alpha"], "meta": {"city": "Berlin"}}

match packet:
    case {"type": "message", "payload": [head, tail]} if head != tail:
        print("mismatch")
    case {"type": "message", "meta": {"city": city}}:
        print(city)
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

	if stdout.String() != "Berlin\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "Berlin\n", stdout.String())
	}
}

func TestInterpreterSupportsUnpackingAssignmentsAndLoops(t *testing.T) {
	source := `first, second = ["Ada", "Lovelace"]
print(first)
print(second)

total = 0
labels = ""
for index, label in zip([1, 2, 3], ["a", "b", "c"]):
    total = total + index
    labels = labels + label

print(total)
print(labels)
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

	want := "Ada\nLovelace\n6\nabc\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
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

func TestInterpreterRejectsInvalidStructuredReturnTypes(t *testing.T) {
	source := `def describe_weather(city: string) -> dict{city: string, temp_c: int, alerts: list[string]}:
    Return a weather summary object for ${city}.

print(json(describe_weather("Berlin")))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":{"city":123,"temp_c":"warm","alerts":"none"}}`,
		},
	}

	interpreter := NewInterpreter(Config{Model: client})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if !strings.Contains(err.Error(), "alerts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreterSendsStructuredSchemaForTypedReturns(t *testing.T) {
	source := `def describe_weather(city: string) -> dict{city: string, temp_c: int, alerts: list[string]}:
    Return a weather summary object for ${city}.

print(json(describe_weather("Berlin")))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":{"city":"Berlin","temp_c":19,"alerts":["clear"]}}`,
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

	if len(client.requests) != 1 {
		t.Fatalf("expected 1 model request, got %d", len(client.requests))
	}

	variants, ok := client.requests[0].JSONSchema["oneOf"].([]any)
	if !ok || len(variants) == 0 {
		t.Fatalf("request schema did not expose oneOf variants: %#v", client.requests[0].JSONSchema)
	}
	returnVariant, ok := variants[0].(map[string]any)
	if !ok {
		t.Fatalf("request schema return variant had unexpected type: %#v", variants[0])
	}

	valueSchema, ok := nestedMap(returnVariant, "properties", "value")
	if !ok {
		t.Fatalf("request schema did not contain return properties.value: %#v", client.requests[0].JSONSchema)
	}
	properties, ok := valueSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("value schema did not expose typed object properties: %#v", valueSchema)
	}
	if _, ok := properties["city"]; !ok {
		t.Fatalf("value schema did not describe city: %#v", valueSchema)
	}
	if _, ok := properties["temp_c"]; !ok {
		t.Fatalf("value schema did not describe temp_c: %#v", valueSchema)
	}
	if _, ok := properties["alerts"]; !ok {
		t.Fatalf("value schema did not describe alerts: %#v", valueSchema)
	}
}

func TestInterpreterSupportsOptionalStructuredFields(t *testing.T) {
	source := `def describe_weather(city: string) -> dict{city: string, alerts: optional[list[string]]}:
    Return a partial weather object for ${city}.

print(json(describe_weather("Berlin")))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":{"city":"Berlin"}}`,
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

	if stdout.String() != "{\"alerts\":null,\"city\":\"Berlin\"}\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "{\"alerts\":null,\"city\":\"Berlin\"}\n", stdout.String())
	}
}

func TestInterpreterSupportsTupleReturnTypes(t *testing.T) {
	source := `def build_pair() -> tuple[string, int]:
    Return a two item tuple where the first item is "latency" and the second item is 42.

print(json(build_pair()))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":["latency","42"]}`,
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

	if stdout.String() != "[\"latency\",42]\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "[\"latency\",42]\n", stdout.String())
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

func TestInterpreterInterpolatesSlicesInAIFunctionBodies(t *testing.T) {
	source := `def describe(items: list[string], digits: string) -> string:
    Mention ${items[1:3]} and ${digits[:5]}.

print(describe(["alpha", "beta", "gamma", "delta"], "31415926535"))
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
	if !strings.Contains(client.prompts[0], `Mention ["beta","gamma"] and 31415.`) {
		t.Fatalf("prompt did not interpolate slices:\n%s", client.prompts[0])
	}
}

func TestInterpreterAIFunctionCapturesMutableValuesAtDefinitionTime(t *testing.T) {
	source := `items = ["alpha", "beta"]
def describe() -> string:
    Return exactly: ${items[0]} and ${items[1]}.

items[0] = "later"
items[1] = "mutated"
print(describe())
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"captured"}`,
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

	if stdout.String() != "captured\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "captured\n", stdout.String())
	}

	if len(client.prompts) != 1 {
		t.Fatalf("expected 1 model prompt, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "Return exactly: alpha and beta.") {
		t.Fatalf("prompt did not preserve definition-time captured values:\n%s", client.prompts[0])
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

func TestInterpreterRejectsSelfRecursiveAIToolCalls(t *testing.T) {
	source := `def summarize_weather(city: string, tone: string = "crisp") -> string:
    Write one ${tone} sentence about the weather in ${city}.

print(summarize_weather("Berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"summarize_weather","arguments":{"city":"Berlin","tone":"crisp"}}}`,
			`{"action":"return","value":"Berlin weather stays crisp and mild today."}`,
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

	if stdout.String() != "Berlin weather stays crisp and mild today.\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "Berlin weather stays crisp and mild today.\n", stdout.String())
	}

	if len(client.prompts) != 2 {
		t.Fatalf("expected 2 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[1], "rejected") {
		t.Fatalf("second prompt did not include the rejected self-call history:\n%s", client.prompts[1])
	}
}

func TestInterpreterFailsFastWhenModelRepeatsRejectedSelfCall(t *testing.T) {
	source := `def summarize_weather(city: string, tone: string = "crisp") -> string:
    Write one ${tone} sentence about the weather in ${city}.

print(summarize_weather("Berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"summarize_weather","arguments":{"city":"Berlin","tone":"crisp"}}}`,
			`{"action":"call","call":{"name":"summarize_weather","arguments":{"city":"Berlin","tone":"crisp"}}}`,
		},
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{
		Model:      client,
		Stdout:     &stdout,
		MaxAISteps: 6,
	})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if !strings.Contains(err.Error(), "repeatedly requested the rejected helper") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreterFailsFastWhenNativeToolCallsRepeatRejectedSelfCall(t *testing.T) {
	source := `def summarize_weather(city: string, tone: string = "crisp") -> string:
    Write one ${tone} sentence about the weather in ${city}.

print(summarize_weather("Berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		queued: []model.Response{
			{
				ToolCalls: []model.ToolCall{
					{Name: "summarize_weather", Arguments: map[string]any{"city": "Berlin", "tone": "crisp"}},
				},
			},
			{
				ToolCalls: []model.ToolCall{
					{Name: "summarize_weather", Arguments: map[string]any{"city": "Berlin", "tone": "crisp"}},
				},
			},
		},
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{
		Model:      client,
		Stdout:     &stdout,
		MaxAISteps: 6,
	})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if !strings.Contains(err.Error(), "repeatedly requested the rejected helper") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreterRejectsIndirectRecursiveAIReentryEvenWithDifferentArguments(t *testing.T) {
	source := `def summarize_weather(city: string, tone: string = "crisp") -> string:
    Write one ${tone} sentence about the weather in ${city}.

def refine_weather(city: string, tone: string = "gentle") -> string:
    Refine the weather summary for ${city} in a ${tone} tone.

print(summarize_weather("Berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"refine_weather","arguments":{"city":"Berlin","tone":"gentle"}}}`,
			`{"action":"call","call":{"name":"summarize_weather","arguments":{"city":"Berlin","tone":"brief"}}}`,
			`{"action":"return","value":"Berlin stays bright with a brief, steady forecast."}`,
			`{"action":"return","value":"Berlin stays bright with a brief, steady forecast."}`,
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

	want := "Berlin stays bright with a brief, steady forecast.\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}

	if len(client.prompts) != 4 {
		t.Fatalf("expected 4 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[2], "already active") {
		t.Fatalf("expected rejection history in follow-up prompt:\n%s", client.prompts[2])
	}
}

func TestInterpreterAIDirectivesLimitToolsAndOverrideModelRequest(t *testing.T) {
	source := `def normalize(city: string) -> string:
    @temperature 0
    @max_tokens 64
    @max_steps 3
    @tools upper
    Turn ${city} into uppercase text.

print(normalize("berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"read_file","arguments":{"path":"ignored.txt"}}}`,
			`{"action":"call","call":{"name":"upper","arguments":{"text":"berlin"}}}`,
			`{"action":"return","value":"BERLIN"}`,
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

	if stdout.String() != "BERLIN\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "BERLIN\n", stdout.String())
	}
	if len(client.requests) != 3 {
		t.Fatalf("expected 3 model requests, got %d", len(client.requests))
	}
	if client.requests[0].Temperature == nil || *client.requests[0].Temperature != 0 {
		t.Fatalf("expected per-function temperature override, got %#v", client.requests[0].Temperature)
	}
	if client.requests[0].MaxTokens == nil || *client.requests[0].MaxTokens != 64 {
		t.Fatalf("expected per-function max token override, got %#v", client.requests[0].MaxTokens)
	}
	if strings.Contains(client.prompts[0], "read_file(path: string)") {
		t.Fatalf("prompt unexpectedly exposed filtered helper:\n%s", client.prompts[0])
	}
	if !strings.Contains(client.prompts[0], "upper(text: string) -> string") {
		t.Fatalf("prompt did not include allowlisted helper:\n%s", client.prompts[0])
	}
	if !strings.Contains(client.prompts[1], "the helper read_file is not enabled for this AI function") {
		t.Fatalf("expected rejection history in second prompt:\n%s", client.prompts[1])
	}
}

func TestInterpreterAIDirectivesRouteModelClientAndReuseConfiguredClient(t *testing.T) {
	t.Setenv("VIBE_REMOTE_API_KEY", "secret-token")

	source := `def summarize(city: string) -> string:
    @provider openai-compatible
    @endpoint https://models.example.test/v1
    @model hosted-gemma
    @api_key_env VIBE_REMOTE_API_KEY
    @timeout_ms 3210
    Return a terse city summary for ${city}.

print(summarize("Berlin"))
print(summarize("Paris"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var configs []model.Config
	var routedClient *scriptedClient
	factory := func(config model.Config) (model.Client, error) {
		configs = append(configs, config)
		routedClient = &scriptedClient{
			responses: []string{
				`{"action":"return","value":"Berlin via hosted route"}`,
				`{"action":"return","value":"Paris via hosted route"}`,
			},
		}
		return routedClient, nil
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{
		ModelConfig: model.Config{
			Provider: "ollama",
			Model:    "gemma4",
			Endpoint: "http://127.0.0.1:11434",
			Timeout:  2 * time.Minute,
		},
		ModelFactory: factory,
		Stdout:       &stdout,
	})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if stdout.String() != "Berlin via hosted route\nParis via hosted route\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "Berlin via hosted route\nParis via hosted route\n", stdout.String())
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 routed client construction, got %d", len(configs))
	}
	if configs[0].Provider != "openai-compatible" {
		t.Fatalf("unexpected provider %#v", configs[0])
	}
	if configs[0].Endpoint != "https://models.example.test/v1" {
		t.Fatalf("unexpected endpoint %#v", configs[0])
	}
	if configs[0].Model != "hosted-gemma" {
		t.Fatalf("unexpected model %#v", configs[0])
	}
	if configs[0].APIKey != "secret-token" {
		t.Fatalf("unexpected api key %#v", configs[0])
	}
	if configs[0].Timeout != 3210*time.Millisecond {
		t.Fatalf("unexpected timeout %#v", configs[0])
	}
	if routedClient == nil || len(routedClient.prompts) != 2 {
		t.Fatalf("expected reused routed client with 2 prompts")
	}
}

func TestInterpreterAIDirectivesOverrideMaxSteps(t *testing.T) {
	source := `def normalize(city: string) -> string:
    @max_steps 1
    @tools upper
    Turn ${city} into uppercase text.

print(normalize("berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call","call":{"name":"upper","arguments":{"text":"berlin"}}}`,
		},
	}

	interpreter := NewInterpreter(Config{Model: client})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if !strings.Contains(err.Error(), "maximum AI tool steps of 1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreterAICacheDirectiveMemoizesResults(t *testing.T) {
	source := `def normalize(city: string) -> string:
    @temperature 0
    @cache true
    Return ${city} in uppercase.

print(normalize("berlin"))
print(normalize("berlin"))
print(cache_stats()["entries"])
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"BERLIN"}`,
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

	if stdout.String() != "BERLIN\nBERLIN\n1\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "BERLIN\nBERLIN\n1\n", stdout.String())
	}
	if len(client.prompts) != 1 {
		t.Fatalf("expected 1 model prompt with cache hit, got %d", len(client.prompts))
	}
}

func TestInterpreterCacheClearRemovesMemoizedEntries(t *testing.T) {
	source := `def normalize(city: string) -> string:
    @temperature 0
    @cache true
    Return ${city} in uppercase.

print(normalize("berlin"))
print(cache_clear())
print(normalize("berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":"BERLIN"}`,
			`{"action":"return","value":"BERLIN"}`,
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

	if stdout.String() != "BERLIN\n1\nBERLIN\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "BERLIN\n1\nBERLIN\n", stdout.String())
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected cache clear to force a second model prompt, got %d", len(client.prompts))
	}
}

func TestInterpreterTryExceptFinally(t *testing.T) {
	source := `try:
    fail("boom")
except err:
    print(err)
finally:
    print("done")
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

	want := "boom\ndone\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterTextBuiltins(t *testing.T) {
	source := `print(base64_encode("hello"))
print(base64_decode("aGVsbG8="))
print(url_encode("hello world?"))
print(url_decode("hello+world%3F"))
print(html_escape("<main data-city=\"Berlin\">"))
print(template_render("Hello ${user.name} from ${city}.", {"user": {"name": "Ada"}, "city": "Berlin"}))
print(sha256("abc"))
print(regex_match("b.", "abc"))
print(regex_find_all("[a-z]+", "go123lang"))
print(regex_replace("[0-9]+", "go123lang", "-"))
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
		"aGVsbG8=",
		"hello",
		"hello+world%3F",
		"hello world?",
		"&lt;main data-city=&#34;Berlin&#34;&gt;",
		"Hello Ada from Berlin.",
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"true",
		`["go","lang"]`,
		"go-lang",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsCSVTimeAndUUIDBuiltins(t *testing.T) {
	source := `rows = csv_parse("name,role\nAda,builder\nGrace,scientist\n")
matrix = csv_parse("a,b\nc,d\n", header=false)
parsed = time_parse("2026-04-17T12:34:56Z")

print(rows[0]["name"])
print(rows[1]["role"])
print(csv_stringify(rows))
print(matrix[1][0])
print(csv_stringify(matrix, header=false))
print(parsed["year"])
print(parsed["unix"])
print(time_format("2026-04-17T12:34:56Z", layout="date"))
print(time_add("2026-04-17T12:34:56Z", "90m"))
print(time_diff("2026-04-17T12:34:56Z", "2026-04-17T14:04:56Z"))
print(duration_parse("1h30m"))
print(uuid_v4())
print(uuid_v7())
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

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 16 {
		t.Fatalf("expected 16 lines, got %d: %q", len(lines), stdout.String())
	}

	wantPrefix := []string{
		"Ada",
		"scientist",
		"name,role",
		"Ada,builder",
		"Grace,scientist",
		"c",
		"a,b",
		"c,d",
		"2026",
		"1776429296",
		"2026-04-17",
		"2026-04-17T14:04:56Z",
		"5400000",
	}
	for index, want := range wantPrefix {
		if lines[index] != want {
			t.Fatalf("unexpected line %d\nwant: %q\ngot:  %q", index, want, lines[index])
		}
	}

	if lines[13] != "5400000" {
		t.Fatalf("unexpected duration_parse output: %q", lines[13])
	}

	uuidV4Pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidV4Pattern.MatchString(lines[14]) {
		t.Fatalf("unexpected uuid_v4 output: %q", lines[14])
	}
	uuidV7Pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidV7Pattern.MatchString(lines[15]) {
		t.Fatalf("unexpected uuid_v7 output: %q", lines[15])
	}
}

func TestInterpreterCollectionHelpers(t *testing.T) {
	source := `pairs = dict_items({"b": 2, "a": 1})
indexed = enumerate(["alpha", "beta"], start=3)
paired = zip(["a", "b", "c"], [1, 2])

print(json(pairs))
print(json(indexed))
print(json(paired))
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
		`[{"key":"a","value":1},{"key":"b","value":2}]`,
		`[{"index":3,"value":"alpha"},{"index":4,"value":"beta"}]`,
		`[["a",1],["b",2]]`,
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterZipStrictModeErrorsOnLengthMismatch(t *testing.T) {
	source := `print(zip([1, 2], [3], strict=true))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	interpreter := NewInterpreter(Config{})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if !strings.Contains(err.Error(), "zip strict mode requires lists of equal length") {
		t.Fatalf("unexpected error: %v", err)
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

func TestInterpreterSupportsConcurrencyPrimitives(t *testing.T) {
	source := `ch = channel(1)
channel_send(ch, "ready")
packet = channel_recv(ch)

mu = mutex()
mutex_lock(mu)
mutex_unlock(mu)

wg = wait_group()
wait_group_add(wg, 1)
task = spawn(str, args=[42], wait_group=wg)
wait_group_wait(wg)

print(packet["value"])
print(await_task(task))
snapshot = metrics_snapshot()
print(snapshot["tasks_spawned_total"] >= 1)
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

	if stdout.String() != "ready\n42\ntrue\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "ready\n42\ntrue\n", stdout.String())
	}
}

func TestInterpreterSupportsAssertStatements(t *testing.T) {
	source := `assert len([1, 2, 3]) == 3
print("ok")
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

	if stdout.String() != "ok\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "ok\n", stdout.String())
	}
}

func TestInterpreterAssertStatementIncludesMessage(t *testing.T) {
	source := `assert false, "expected the condition to hold"
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	interpreter := NewInterpreter(Config{})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if !strings.Contains(err.Error(), "expected the condition to hold") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreterExposesRuntimeMetrics(t *testing.T) {
	source := `snapshot = runtime_metrics()
print(snapshot["go.goroutine.count"] >= 1)
print(runtime_metric("go.goroutine.count", 0) >= 1)
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

	if stdout.String() != "true\ntrue\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "true\ntrue\n", stdout.String())
	}
}

func TestInterpreterSupportsNativeMultipleToolCalls(t *testing.T) {
	source := `def summarize_weather(city: string) -> string:
    Write one sentence about the weather in ${city}.

print(summarize_weather("Berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		queued: []model.Response{
			{
				ToolCalls: []model.ToolCall{
					{Name: "upper", Arguments: map[string]any{"text": "berlin"}},
					{Name: "replace", Arguments: map[string]any{"text": "stormy", "old": "stormy", "new": "clear"}},
				},
			},
			{
				Text: `{"action":"return","value":"BERLIN stays clear today."}`,
			},
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

	if stdout.String() != "BERLIN stays clear today.\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "BERLIN stays clear today.\n", stdout.String())
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected 2 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[1], "upper({\"text\":\"berlin\"}) => \"BERLIN\"") {
		t.Fatalf("follow-up prompt did not include first tool result:\n%s", client.prompts[1])
	}
	if !strings.Contains(client.prompts[1], "replace({\"new\":\"clear\",\"old\":\"stormy\",\"text\":\"stormy\"}) => \"clear\"") {
		t.Fatalf("follow-up prompt did not include second tool result:\n%s", client.prompts[1])
	}
}

func TestInterpreterSupportsJSONCallManyActions(t *testing.T) {
	source := `def summarize_weather(city: string) -> string:
    Write one sentence about the weather in ${city}.

print(summarize_weather("Berlin"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"call_many","calls":[{"name":"upper","arguments":{"text":"berlin"}},{"name":"replace","arguments":{"text":"stormy","old":"stormy","new":"clear"}}]}`,
			`{"action":"return","value":"BERLIN stays clear today."}`,
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

	if stdout.String() != "BERLIN stays clear today.\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "BERLIN stays clear today.\n", stdout.String())
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected 2 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[1], "upper({\"text\":\"berlin\"}) => \"BERLIN\"") {
		t.Fatalf("follow-up prompt did not include first tool result:\n%s", client.prompts[1])
	}
	if !strings.Contains(client.prompts[1], "replace({\"new\":\"clear\",\"old\":\"stormy\",\"text\":\"stormy\"}) => \"clear\"") {
		t.Fatalf("follow-up prompt did not include second tool result:\n%s", client.prompts[1])
	}
}

func TestInterpreterRunsDeferredExpressionsInLIFOOrder(t *testing.T) {
	source := `print("start")
defer print("first cleanup")
defer print("second cleanup")
print("done")
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

	want := "start\ndone\nsecond cleanup\nfirst cleanup\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterRunsDeferredExpressionsOnErrorAndCapturesValues(t *testing.T) {
	source := `name = "Ada"
defer print(name)
name = "Grace"
fail("boom")
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	err = interpreter.Execute(context.Background(), program)
	if err == nil {
		t.Fatalf("Execute returned nil error")
	}
	if err.Error() != "boom" {
		t.Fatalf("unexpected error: %v", err)
	}

	if stdout.String() != "Ada\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "Ada\n", stdout.String())
	}
}

func TestInterpreterRunsDeferredExpressionsOnContinue(t *testing.T) {
	source := `for value in [1, 2]:
    defer print("cleanup " + str(value))
    if value == 1:
        continue
    print("body " + str(value))
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

	want := "cleanup 1\nbody 2\ncleanup 2\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsChannelSelect(t *testing.T) {
	source := `first = channel(1)
second = channel(1)
channel_send(second, "beta")
selected = channel_select([first, second])
closed = channel(0)
channel_close(closed)
closed_packet = channel_select([closed], timeout_ms=1)
timeout_packet = channel_select([first], timeout_ms=1)

print(selected["channel"] == second)
print(selected["value"])
print(selected["closed"])
print(closed_packet["closed"])
print(timeout_packet["timeout"])
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

	if stdout.String() != "true\nbeta\nfalse\ntrue\ntrue\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "true\nbeta\nfalse\ntrue\ntrue\n", stdout.String())
	}
}

func TestInterpreterSupportsURLAndJSONHTTPHelpers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		defer request.Body.Close()

		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, fmt.Sprintf(
			`{"ok":true,"request":{"method":%q,"content_type":%q,"body":%s}}`,
			request.Method,
			request.Header.Get("Content-Type"),
			string(body),
		))
	}))
	defer server.Close()

	source := fmt.Sprintf(`parsed = url_parse("https://ada.example:8443/products/view?tag=lang&tag=ai&sort=desc#hero")
encoded = query_encode({"tag": ["lang", "ai"], "sort": "desc"})
decoded = query_decode("tag=lang&tag=ai&sort=desc")
rebuilt = url_build({"scheme": parsed["scheme"], "host": parsed["host"], "path": parsed["path"], "query": parsed["query"], "fragment": parsed["fragment"]})
response = http_request_json(%q, method="POST", body={"name": "Ada", "roles": ["builder", "tester"]})

print(parsed["hostname"])
print(parsed["port"])
print(parsed["query"]["tag"][1])
print(encoded)
print(decoded["sort"])
print(rebuilt)
print(response["status"])
print(response["json"]["ok"])
print(response["json"]["request"]["method"])
print(response["json"]["request"]["content_type"])
print(response["json"]["request"]["body"]["name"])
print(response["json"]["request"]["body"]["roles"][1])
`, server.URL)

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
		"ada.example",
		"8443",
		"ai",
		"sort=desc&tag=lang&tag=ai",
		"desc",
		"https://ada.example:8443/products/view?sort=desc&tag=lang&tag=ai#hero",
		"200",
		"true",
		"POST",
		"application/json",
		"Ada",
		"tester",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsComprehensions(t *testing.T) {
	source := `item = "outside"
names = [upper(name) for name in ["ada", "grace", "linus"] if "a" in name]
lengths = {name: len(name) for name in names if len(name) > 3}

print(item)
print(json(names))
print(json(lengths))
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

	if stdout.String() != "outside\n[\"ADA\",\"GRACE\"]\n{\"GRACE\":5}\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "outside\n[\"ADA\",\"GRACE\"]\n{\"GRACE\":5}\n", stdout.String())
	}
}

func TestInterpreterSupportsAIHTTPServer(t *testing.T) {
	source := `def handle(request: dict) -> dict:
    Return a dict with status 201, headers {"content-type": "text/plain"}, and body "hello from ai".

server = http_serve("127.0.0.1:0", handle)
response = http_request("http://" + server["address"] + "/hello")
print(response["status"])
print(response["body"])
http_server_stop(server["handle"])
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":{"status":201,"headers":{"content-type":"text/plain"},"body":"hello from ai"}}`,
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

	if stdout.String() != "201\nhello from ai\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "201\nhello from ai\n", stdout.String())
	}
	if len(client.prompts) != 1 {
		t.Fatalf("expected 1 model prompt, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[0], "\"path\": \"/hello\"") {
		t.Fatalf("prompt did not include request path:\n%s", client.prompts[0])
	}
}

func TestInterpreterSupportsStaticHTTPResponses(t *testing.T) {
	tempDir := t.TempDir()
	siteDir := filepath.Join(tempDir, "site")
	if err := os.MkdirAll(filepath.Join(siteDir, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("<h1>vibelang</h1>"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "pkg", "app.wasm"), []byte{0x00, 0x61, 0x73, 0x6d}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := fmt.Sprintf(`root = %q

home = http_static_response(root=root, request={"path": "/"}, cache_control="public, max-age=60")
wasm = http_static_response(root=root, request={"path": "/pkg/app.wasm"})
missing = http_static_response(root=root, request={"path": "/missing.txt"})
print(home["status"])
print(home["headers"]["Content-Type"])
print(home["headers"]["Cache-Control"])
print(contains(home["body"], "vibelang"))
print(wasm["headers"]["Content-Type"])
print(len(wasm["body"]))
print(missing["status"])
`, siteDir)

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := "200\ntext/html; charset=utf-8\npublic, max-age=60\ntrue\napplication/wasm\n4\n404\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestFormatHTTPHandlerResponseNormalizesContentTypeHeader(t *testing.T) {
	interpreter := NewInterpreter(Config{})
	response, err := interpreter.formatHTTPHandlerResponse(map[string]any{
		"headers": map[string]any{
			"content-type": "text/plain; charset=utf-8",
		},
		"html": "<p>hello</p>",
	})
	if err != nil {
		t.Fatalf("formatHTTPHandlerResponse returned error: %v", err)
	}

	if response.Status != http.StatusOK {
		t.Fatalf("unexpected status %d", response.Status)
	}
	if response.Body != "<p>hello</p>" {
		t.Fatalf("unexpected body %q", response.Body)
	}
	if len(response.Headers) != 1 {
		t.Fatalf("expected exactly 1 header after normalization, got %#v", response.Headers)
	}
	if response.Headers["Content-Type"] != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type header %#v", response.Headers)
	}
}

func TestFormatHTTPHandlerResponseSupportsSSEBatches(t *testing.T) {
	interpreter := NewInterpreter(Config{})
	response, err := interpreter.formatHTTPHandlerResponse(map[string]any{
		"status": 202,
		"sse": []any{
			map[string]any{"event": "status", "data": "booting", "id": "evt-1"},
			"done",
		},
	})
	if err != nil {
		t.Fatalf("formatHTTPHandlerResponse returned error: %v", err)
	}

	if response.Status != http.StatusAccepted {
		t.Fatalf("unexpected status %d", response.Status)
	}
	if response.SSE == nil {
		t.Fatalf("expected SSE payload")
	}
	if response.Headers["Content-Type"] != "text/event-stream; charset=utf-8" {
		t.Fatalf("unexpected content type header %#v", response.Headers)
	}
	if len(response.SSE.Events) != 2 {
		t.Fatalf("expected 2 SSE events, got %d", len(response.SSE.Events))
	}
	if response.SSE.Events[0].Event != "status" || response.SSE.Events[0].Data != "booting" || response.SSE.Events[0].ID != "evt-1" {
		t.Fatalf("unexpected first SSE event %#v", response.SSE.Events[0])
	}
	if response.SSE.Events[1].Data != "done" {
		t.Fatalf("unexpected second SSE event %#v", response.SSE.Events[1])
	}
}

func TestInterpreterSupportsHTTPRouteServer(t *testing.T) {
	source := `def home(request: dict) -> dict:
    Return a dict with status 200, json {"route": "home", "path": request["path"]}.

def user(request: dict) -> dict:
    Return a dict with status 200, json {"route": "user", "id": request["params"]["id"], "method": request["method"]}.

routes = [{"pattern": "/", "handler": home}, {"pattern": "/users/:id", "methods": ["GET"], "handler": user}]

server = http_serve_routes("127.0.0.1:0", routes)
root = http_request_json("http://" + server["address"] + "/")
profile = http_request_json("http://" + server["address"] + "/users/ada")
print(root["json"]["route"])
print(profile["json"]["id"])
print(profile["json"]["method"])
http_server_stop(server["handle"])
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":{"status":200,"json":{"route":"home","path":"/"}}}`,
			`{"action":"return","value":{"status":200,"json":{"route":"user","id":"ada","method":"GET"}}}`,
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

	if stdout.String() != "home\nada\nGET\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "home\nada\nGET\n", stdout.String())
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected 2 model prompts, got %d", len(client.prompts))
	}
	if !strings.Contains(client.prompts[1], `"id": "ada"`) || !strings.Contains(client.prompts[1], `"/users/:id"`) {
		t.Fatalf("route handler prompt did not include extracted params:\n%s", client.prompts[1])
	}
}

func TestInterpreterSupportsSSEHTTPResponsesFromChannels(t *testing.T) {
	source := `stream = channel(2)
channel_send(stream, sse_event("booting", event="status", id="evt-1"))
channel_send(stream, "done")
channel_close(stream)

def handle(request: dict) -> dict:
    Return exactly {"status": 202, "sse_channel": "channel_1"}.

server = http_serve("127.0.0.1:0", handle)
response = http_request("http://" + server["address"] + "/events")
print(response["status"])
print(response["headers"]["Content-Type"])
print(response["body"])
http_server_stop(server["handle"])
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
			`{"action":"return","value":{"status":202,"sse_channel":"channel_1"}}`,
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

	want := "202\ntext/event-stream; charset=utf-8\nevent: status\nid: evt-1\ndata: booting\n\ndata: done\n\n\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsToolCatalogBuiltins(t *testing.T) {
	source := `tools = tool_catalog(prefix="json_")
print(len(tools))
print(tools[0]["name"])
print(tools[1]["name"])

detail = tool_describe("http_request")
print(detail["name"])
print(detail["return_type"])
print(detail["params"][0]["name"])
print(detail["kind"])
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

	want := "2\njson_parse\njson_pretty\nhttp_request\ndict\nurl\nbuiltin\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsURLImports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/shared.vibe":
			_, _ = io.WriteString(writer, "prefix = \"remote\"\n")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	source := fmt.Sprintf(`import %q as remote
print(remote.prefix)
`, server.URL+"/shared.vibe")

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if stdout.String() != "remote\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "remote\n", stdout.String())
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

func TestInterpreterImportsModulesWithDotAccess(t *testing.T) {
	tempDir := t.TempDir()
	modulePath := filepath.Join(tempDir, "shared.vibe")
	moduleSource := `prefix = "Dr."
def format_name(name: string) -> string:
    Return exactly: ${prefix} ${name}
`
	if err := os.WriteFile(modulePath, []byte(moduleSource), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	source := `import "./shared.vibe" as shared
print(shared.prefix)
print(shared.format_name("Ada"))
`

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	client := &scriptedClient{
		responses: []string{
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

	if stdout.String() != "Dr.\nDr. Ada\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "Dr.\nDr. Ada\n", stdout.String())
	}
}

func TestInterpreterBooleanOperatorsReturnPythonStyleValues(t *testing.T) {
	source := `print("left" and "right")
print("" or "fallback")
print(0 or 7)
print("value" or "ignored")
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

	want := "right\nfallback\n7\nvalue\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterSupportsNegativeIndexes(t *testing.T) {
	source := `items = ["alpha", "beta", "gamma"]
print(items[-1])
print(items[-2])
print("vibe"[-1])
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

	if stdout.String() != "gamma\nbeta\ne\n" {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", "gamma\nbeta\ne\n", stdout.String())
	}
}

func TestInterpreterSupportsSlices(t *testing.T) {
	source := `items = ["alpha", "beta", "gamma", "delta"]
print(items[1:3])
print(items[:2])
print(items[2:])
print(items[::-1])
print(items[::2])
print("vibelang"[1:5])
print("vibelang"[-4:-1])
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
		`["beta","gamma"]`,
		`["alpha","beta"]`,
		`["gamma","delta"]`,
		`["delta","gamma","beta","alpha"]`,
		`["alpha","gamma"]`,
		"ibel",
		"lan",
		"",
	}, "\n")
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

func TestInterpreterProvidesRouteMatchingHelpers(t *testing.T) {
	source := `user = route_match("/users/:id", "/users/42")
asset = route_match("/assets/*path", "/assets/css/app.css")
miss = route_match("/users/:id", "/teams/42")
normalized = route_match("users/:id", "users/99/")
encoded = route_match("/files/:name", "/files/report%20final.txt")

print(user["matched"])
print(user["params"]["id"])
print(asset["matched"])
print(asset["params"]["path"])
print(miss["matched"])
print(json(miss["params"]))
print(normalized["matched"])
print(normalized["params"]["id"])
print(encoded["params"]["name"])
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
		"true",
		"42",
		"true",
		"css/app.css",
		"false",
		"{}",
		"true",
		"99",
		"report final.txt",
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

func TestInterpreterProvidesSocketListenerStdlib(t *testing.T) {
	source := `listener = socket_listen("127.0.0.1:0")
accept_task = spawn(socket_accept, args=[listener["handle"]])
client = socket_open(listener["address"])
accepted = await_task(accept_task)
socket_write(client, "ping")
print(accepted["ok"])
print(socket_read(accepted["handle"]))
socket_write(accepted["handle"], "pong")
print(socket_read(client))
print(socket_remote_addr(accepted["handle"]) != "")
print(socket_local_addr(client) != "")
print(socket_close(accepted["handle"]))
print(socket_close(client))
print(socket_listener_close(listener["handle"]))
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

	want := "true\nping\npong\ntrue\ntrue\ntrue\ntrue\ntrue\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterProvidesYAMLStdlib(t *testing.T) {
	source := fmt.Sprintf(`path = %q
write_yaml(path, {"name": "Ada", "enabled": true, "ports": [8080, 8081]})
payload = read_yaml(path)
parsed = yaml_parse("service: vibelang\nports:\n  - 7000\n  - 7001\n")

print(payload["name"])
print(payload["enabled"])
print(payload["ports"][1])
print(parsed["service"])
print(parsed["ports"][0])
print(contains(yaml_stringify(payload), "name: Ada"))
`, filepath.Join(t.TempDir(), "config.yaml"))

	program, err := parser.ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource returned error: %v", err)
	}

	var stdout bytes.Buffer
	interpreter := NewInterpreter(Config{Stdout: &stdout})
	if err := interpreter.Execute(context.Background(), program); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := "Ada\ntrue\n8081\nvibelang\n7000\ntrue\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestInterpreterProvidesCookieHelpers(t *testing.T) {
	source := `cookie = cookie_build("session", "abc123", {"path": "/", "http_only": true, "same_site": "lax", "max_age": 60, "secure": true})
parsed = cookie_parse("theme=dark; session=abc123")

print(contains(cookie, "session=abc123"))
print(contains(cookie, "Path=/"))
print(contains(cookie, "Max-Age=60"))
print(contains(cookie, "HttpOnly"))
print(contains(cookie, "Secure"))
print(contains(cookie, "SameSite=Lax"))
print(parsed["theme"])
print(parsed["session"])
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

	want := "true\ntrue\ntrue\ntrue\ntrue\ntrue\ndark\nabc123\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout\nwant: %q\ngot:  %q", want, stdout.String())
	}
}
