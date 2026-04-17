# Tutorial: Run Your First vibelang Program

This tutorial is for developers who want to get a first `vibelang` program running end to end with a model backend, starting with the local path.

## Goal

By the end, you will:

- start a local model server
- build the interpreter
- run a small `.vibe` program
- import one `.vibe` module from another
- inspect the AI execution trace

## 1. Start a Local Model

Choose one backend.

With Ollama:

```bash
ollama serve
ollama pull gemma4
```

With `llama.cpp`:

```bash
llama-server -m /models/gemma4.gguf --port 8080
```

## 2. Build the Interpreter

From the repository root:

```bash
go build -o bin/vibelang ./cmd/vibelang
```

## 3. Write a Program

Create a file named `hello.vibe`:

```python
def greet(name: string, tone: string = "upbeat") -> string:
    Write a short, ${tone} greeting for ${name}.
    Keep it to one sentence.

name = "Ada"
message = greet(name=name)
print(message)
```

The function body is plain text. `vibelang` passes the bound inputs to the model, sends a structured JSON schema to the local backend, and then coerces the returned value to the declared type.

Calls can also mix positional and keyword arguments:

```python
print(greet("Ada"))
print(greet(name="Ada", tone="playful"))
```

When you need tighter control over the AI runtime, put directives at the top of the body:

```python
def slugify(title: string) -> string:
    @temperature 0
    @max_steps 4
    @cache true
    @tools lower, trim, replace, regex_replace
    Convert ${title} into a lowercase URL slug.
    Replace whitespace runs with "-".
```

When a helper is deterministic and likely to repeat, `@cache true` memoizes successful AI results for the current run:

```python
def normalize_city(city: string) -> string:
    @temperature 0
    @cache true
    Return ${city} in uppercase letters.

print(normalize_city("berlin"))
print(normalize_city("berlin"))
print(cache_stats()["entries"])
```

Inline prompts also work without defining a function first:

```python
workspace = join_path([cwd(), "tmp"])
make_dir(workspace)
path = join_path([workspace, "pi.txt"])
digits = * return the first 5 digits of pi as a string without explanation.

if * check whether ${path} exists:
    * delete the file at ${path}.
else:
    * write ${digits} to the file at ${path}.
```

Prompt templates can interpolate full expressions:

```python
def explain_file(path: string, digits: string) -> string:
    Write one short line about ${basename(path)}.
    Mention that ${digits} has ${len(digits)} characters.
```

The expression engine also supports Python-style slicing:

```python
digits = "31415926535"
items = ["alpha", "beta", "gamma", "delta"]

print(digits[:5])
print(items[1:3])
print(items[::-1])
```

Comprehensions are available too:

```python
names = [upper(name) for name in ["ada", "grace", "linus"] if "a" in name]
lengths = {name: len(name) for name in names if len(name) > 3}

print(json(names))
print(json(lengths))
```

Structural pattern matching is available for deterministic branching on data shape:

```python
packet = {"type": "message", "payload": ["alpha", "beta"], "meta": {"city": "Berlin"}}

match packet:
    case {"type": "ping"}:
        print("pong")
    case {"type": "message", "payload": [head, tail], "meta": {"city": city}}:
        print(head)
        print(tail)
        print(city)
```

Modules work with ordinary files:

```python
# shared.vibe
prefix = "Dr."

def format_name(name: string) -> string:
    Return exactly: ${prefix} ${name}
```

```python
# main.vibe
from "./shared.vibe" import prefix, format_name
import "./shared.vibe" as shared

print(prefix)
print(format_name("Ada"))
print(shared.format_name("Grace"))
```

Bundled `std` modules can be imported directly:

```python
import "std/web" as web
import "std/ai" as ai

def handle(request: dict) -> dict:
    Call web.render_app_shell with the title "demo", the route ${request["path"]}, and initial state {"path": request["path"]}.
    Return a dict with html set to that app shell.

summary = ai.summarize_payload({"route": "/demo", "status": 200})
print(summary)
```

The runtime also has native concurrency primitives:

```python
wg = wait_group()
wait_group_add(wg, 1)
task = spawn(str, args=[42], wait_group=wg)
wait_group_wait(wg)
print(await_task(task))
```

And channel selection for Go-like coordination:

```python
first = channel(1)
second = channel(1)
channel_send(second, "ready")
packet = channel_select([first, second], timeout_ms=10)
print(packet["value"])
```

Deterministic code can catch runtime failures:

```python
try:
    fail("simulated failure")
except err:
    print(err)
finally:
    print("cleanup complete")
```

For cleanup that should happen whenever a block exits, use `defer`:

```python
for name in ["alpha", "beta"]:
    path = join_path([cwd(), name + ".tmp"])
    defer delete_file(path)
    write_file(path, name)
    print("created " + basename(path))
```

The runtime also ships URL and JSON HTTP helpers for deterministic request plumbing:

```python
parsed = url_parse("https://ada.example:8443/products/view?tag=lang&tag=ai&sort=desc#hero")
rebuilt = url_build({"scheme": parsed["scheme"], "host": parsed["host"], "path": parsed["path"], "query": parsed["query"], "fragment": parsed["fragment"]})
print(parsed["hostname"])
print(rebuilt)
```

Typed structured outputs make AI functions more reliable because the runtime now turns the declared return type into a stricter JSON schema before it calls the model:

```python
def describe_weather(city: string) -> dict{city: string, summary: string, alerts: optional[list[string]], stats: dict{temp_c: int, wind_kph: int}, focus: tuple[string, int]}:
    Return a compact weather object for ${city}.

print(json_pretty(describe_weather("Berlin")))
```

## 4. Run It

With Ollama:

```bash
./bin/vibelang --provider ollama --model gemma4 hello.vibe
```

With `llama.cpp`:

```bash
./bin/vibelang --provider llamacpp --endpoint http://127.0.0.1:8080 --model gemma4 hello.vibe
```

With a remote OpenAI-compatible provider:

```bash
export OPENAI_API_KEY=...
./bin/vibelang --provider openai --model gpt-4.1-mini hello.vibe
```

You should see a single generated line printed to stdout.

## 5. Turn On Tracing

Tracing is useful when the model decides to call helper functions or when it returns malformed JSON.

```bash
./bin/vibelang --provider ollama --model gemma4 --trace hello.vibe
```

The trace is written to stderr and includes raw model responses and helper-call activity.

## 6. Validate Syntax Without Running AI

When you only want to check parsing or module resolution, use `--check`:

```bash
./bin/vibelang --check main.vibe
```

## Next Steps

- Read the [how-to guide](how-to-run-local-models.md) for backend-specific setup.
- Read the [reference](reference.md) for language syntax and builtins.
- Run [examples/modules/main.vibe](../examples/modules/main.vibe) to see imports and module-backed AI functions.
- Run [examples/keyword_args.vibe](../examples/keyword_args.vibe) to see default parameters and keyword calls.
- Run [examples/match.vibe](../examples/match.vibe) to see structural pattern matching with captures.
- Run [examples/slices.vibe](../examples/slices.vibe) to see slicing on strings and lists.
- Run [examples/comprehensions.vibe](../examples/comprehensions.vibe) to see list and dict comprehensions.
- Run [examples/tool_chain.vibe](../examples/tool_chain.vibe) to see AI tool calls in action.
- Run [examples/pi_file.vibe](../examples/pi_file.vibe) to see inline prompts and filesystem tools together.
- Run [examples/stdlib.vibe](../examples/stdlib.vibe) to see expression-aware prompt interpolation plus the expanded standard library.
- Run [examples/ops.vibe](../examples/ops.vibe) to see globbing, file moves, process execution, and math helpers together.
- Run [examples/concurrency.vibe](../examples/concurrency.vibe) to see spawned tasks, channels, and wait groups.
- Run [examples/select.vibe](../examples/select.vibe) to see `channel_select`.
- Run [examples/http_server.vibe](../examples/http_server.vibe) to see AI-backed HTTP handlers and the bundled `std/web` module.
- Run [examples/structured_outputs.vibe](../examples/structured_outputs.vibe) to see typed AI return values, optional fields, nested records, and tuples.
- Run [examples/directives.vibe](../examples/directives.vibe) to see per-function AI directives.
- Run [examples/error_handling.vibe](../examples/error_handling.vibe) to see `try` / `except` / `finally` and text helpers.
