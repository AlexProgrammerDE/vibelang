# vibelang

`vibelang` is a Python-shaped interpreted language where user-defined function bodies are plain language. The interpreter is written in Go, and every function call is executed by a local LLM running through Ollama or `llama.cpp`.

## What It Does

- Uses indentation-sensitive, Python-like syntax for variables, expressions, loops, and conditionals.
- Treats every `def` body as natural-language instructions instead of imperative code.
- Supports AI macros with `macro` definitions and `@macro(...)` expansion syntax, so the model can synthesize real vibelang expressions on the fly.
- Supports module loading with `import "./module.vibe" as module` and `from "./module.vibe" import helper`.
- Supports Python-style default parameter values and keyword arguments for user-defined functions and builtins.
- Supports inline `* prompt` expressions in assignments, conditions, loops, and standalone statements.
- Supports leading AI directives such as `@temperature`, `@max_tokens`, `@max_steps`, `@cache`, `@tools`, and `@deny_tools` inside function and macro bodies.
- Evaluates `${...}` prompt placeholders as real vibelang expressions, including indexing and prompt-safe builtins such as `len`, `basename`, or `join_path`.
- Supports Python-style list and dict comprehensions with optional trailing `if` filters.
- Supports structural `match` / `case` branching with wildcard, list, dict, and capture patterns.
- Supports structured AI return types such as `dict{city: string, alerts: optional[list[string]]}` and `tuple[string, int]`, and turns them into tighter JSON schemas for model backends.
- Constrains helper calls with per-helper JSON schemas, so models see the exact argument names, required fields, and types for each callable tool.
- Supports deterministic `try` / `except` / `finally` blocks for builtin, tool, and model-call failures.
- Supports block-scoped `defer` expressions for LIFO cleanup on normal exit, `break`, `continue`, and errors.
- Supports Python-like member access for imported modules and dict-shaped values, so `shared.helper()` works naturally.
- Supports Python-style negative indexing and slicing for lists and strings, plus operand-returning `and`/`or` short-circuit behavior.
- Lets AI functions call other AI functions through a strict JSON tool-call loop.
- Rejects self-recursive AI helper calls before they spiral into repeated depth exhaustion, feeds the rejection back into the next model step, and fails fast if the model keeps retrying a rejected helper.
- Adds opt-in AI result caching, first-class sets, richer dict and list helpers, numeric reducers, structured logging, and OpenTelemetry trace export.
- Captures surrounding non-function values by value when an AI function is defined, so later mutations do not silently change prompt inputs.
- Exposes a broader standard library for AI execution, including filesystem, path, JSON, string, environment, globbing, HTTP, TCP clients and listeners, time, math, local process helpers, async tasks, channels, channel selection, mutexes, wait groups, route matching, and runtime metrics.
- Starts AI-backed HTTP servers, including ordered route tables for multi-endpoint apps.
- Resolves modules from relative paths, `VIBE_PATH`, working-directory `std/` modules, direct URLs, and `github.com/owner/repo/path@ref` imports.
- Runs against local or remote model servers, with first-class support for Ollama, `llama.cpp`, OpenAI, Groq, and other OpenAI-compatible gateways.
- Sends chat-style structured JSON requests to local backends, which works better with modern Gemma 4 model servers.
- Caches parsed prompt templates so repeated `${...}` interpolation work does not keep reparsing the same expressions.
- Ships standard-library modules written in vibelang itself, including `std/web`, `std/telemetry`, and `std/ai`.

## Quick Start

Build the interpreter:

```bash
go build -o bin/vibelang ./cmd/vibelang
```

Run the included example with Ollama:

```bash
ollama serve
ollama pull gemma4
./bin/vibelang --provider ollama --model gemma4 examples/hello.vibe
```

Run the same program with `llama.cpp`:

```bash
llama-server -m /models/gemma4.gguf --port 8080
./bin/vibelang --provider llamacpp --endpoint http://127.0.0.1:8080 --model gemma4 examples/hello.vibe
```

If your local model tag or GGUF filename uses a different name, pass that exact value with `--model`.

Run against a remote OpenAI-compatible endpoint:

```bash
export OPENAI_API_KEY=...
./bin/vibelang --provider openai --model gpt-4.1-mini examples/hello.vibe
```

```bash
export GROQ_API_KEY=...
./bin/vibelang --provider groq --model openai/gpt-oss-20b examples/hello.vibe
```

Validate a program without hitting the model:

```bash
./bin/vibelang --check examples/modules/main.vibe
```

Print the interpreter version:

```bash
./bin/vibelang --version
```

## Example

```python
def summarize_weather(city: string, tone: string = "crisp") -> string:
    Write one ${tone} sentence about the weather in ${city}.

city = "Berlin"
forecast = summarize_weather(city=city)
print(forecast)
```

Inline prompts work anywhere a full expression makes sense in statement position:

```python
workspace = join_path([cwd(), "tmp"])
make_dir(workspace)
path = join_path([workspace, "pi.txt"])
digits = * return the first 5 digits of pi as a string without explanation.

if * check whether ${path} already exists:
    * delete the file at ${path}.
else:
    * write ${digits} to the file at ${path}.
```

Prompt interpolation is expression-aware, not just name-aware:

```python
def explain_file(path: string, digits: string, tone: string = "matter-of-fact") -> string:
    Write one ${tone} sentence about ${basename(path)} inside ${dirname(path)}.
    Mention that ${digits} has ${len(digits)} characters.
```

AI bodies can also declare execution controls up front:

```python
def slugify(title: string) -> string:
    @temperature 0
    @max_steps 4
    @cache true
    @tools lower, trim, replace, regex_replace
    Convert ${title} into a lowercase URL slug.
    Replace whitespace runs with "-".
    Remove punctuation and collapse repeated "-" runs.
```

Slices are first-class expressions:

```python
digits = "31415926535"
items = ["alpha", "beta", "gamma", "delta"]

print(digits[:5])
print(digits[-3:])
print(items[1:3])
print(items[::-1])
```

Comprehensions work the way Python users expect:

```python
names = [upper(name) for name in ["ada", "grace", "linus"] if "a" in name]
lengths = {name: len(name) for name in names if len(name) > 3}

print(json(names))
print(json(lengths))
```

Pattern matching lets deterministic code branch on data shape before handing the rest to AI:

```python
packet = {"type": "message", "payload": ["alpha", "beta"], "meta": {"city": "Berlin"}}

match packet:
    case {"type": "ping"}:
        print("pong")
    case {"type": "message", "payload": [head, tail], "meta": {"city": city}}:
        print(head)
        print(tail)
        print(city)
    case _:
        print("fallback")
```

AI macros expand into real expressions before evaluation:

```python
macro even_numbers(limit: int) -> list[int]:
    Return one valid vibelang expression that builds the even numbers below ${limit} * 2.
    Prefer using range with explicit named arguments.

numbers = @even_numbers(5)
print(numbers)
```

Structured outputs can stay Python-shaped while still giving the model a precise target:

```python
def describe_weather(city: string) -> dict{city: string, summary: string, alerts: optional[list[string]], stats: dict{temp_c: int, wind_kph: int}, focus: tuple[string, int]}:
    Return a compact weather object for ${city}.

print(json_pretty(describe_weather("Berlin")))
```

Modules are ordinary `.vibe` files:

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

Concurrent work uses native Go-backed primitives:

```python
ch = channel(1)
channel_send(ch, "tasks queued")

wg = wait_group()
wait_group_add(wg, 2)

first = spawn(str, args=[42], wait_group=wg)
second = spawn(join, args=[["vibe", "lang"], "-"], wait_group=wg)

notice = channel_recv(ch)
wait_group_wait(wg)

print(notice["value"])
print(await_task(first))
print(await_task(second))
```

Channel selection adds a more Go-like coordination primitive:

```python
fast = channel(1)
slow = channel(1)
channel_send(slow, "background")

packet = channel_select([fast, slow], timeout_ms=10)
print(packet["channel"] == slow)
print(packet["value"])
```

Explicit AI caching is useful for expensive deterministic helpers:

```python
def normalize_city(city: string) -> string:
    @temperature 0
    @cache true
    Return ${city} in uppercase letters.

print(normalize_city("berlin"))
print(normalize_city("berlin"))
print(cache_stats()["entries"])
```

HTTP handlers can also be AI-backed:

```python
import "std/web" as web

def handle(request: dict) -> dict:
    Call web.render_app_shell with the title "vibelang demo", the route ${request["path"]}, and initial state {"path": request["path"]}.
    Return a dict with html set to that app shell.

server = http_serve("127.0.0.1:0", handle)
response = http_request("http://" + server["address"] + "/hello")
print(response["status"])
http_server_stop(server["handle"])
```

Ordered route tables keep larger AI-backed services predictable:

```python
def home(request: dict) -> dict:
    Return a dict with status 200, json {"route": "home"}.

def profile(request: dict) -> dict:
    Return a dict with status 200, json {"route": "profile", "id": request["params"]["id"]}.

routes = [{"pattern": "/", "handler": home}, {"pattern": "/users/:id", "methods": ["GET"], "handler": profile}]
server = http_serve_routes("127.0.0.1:0", routes)
```

TCP listener handles let deterministic code accept sockets while AI stays focused on the protocol logic:

```python
listener = socket_listen("127.0.0.1:0")
accept_task = spawn(socket_accept, args=[listener["handle"]])
client = socket_open(listener["address"])
accepted = await_task(accept_task)

socket_write(client, "ping")
print(socket_read(accepted["handle"]))
socket_listener_close(listener["handle"])
```

Deterministic code can recover from runtime errors without dropping back to the host shell:

```python
try:
    fail("simulated failure")
except err:
    print(err)
finally:
    print("cleanup complete")
```

Block-scoped cleanup is available without wrapping everything in `try` / `finally`:

```python
for name in ["alpha", "beta"]:
    path = join_path([cwd(), name + ".tmp"])
    defer delete_file(path)
    write_file(path, name)
    print("created " + basename(path))
```

URL helpers and JSON-first HTTP helpers keep API plumbing deterministic:

```python
parsed = url_parse("https://ada.example:8443/products/view?tag=lang&tag=ai&sort=desc#hero")
rebuilt = url_build({"scheme": parsed["scheme"], "host": parsed["host"], "path": parsed["path"], "query": parsed["query"], "fragment": parsed["fragment"]})
response = http_request_json("https://example.com/api", method="POST", body={"name": "Ada"})

print(parsed["hostname"])
print(rebuilt)
print(response["json"])
```

Route matching is built in for AI-backed HTTP handlers and plain deterministic code:

```python
user = route_match("/users/:id", "/users/42")
assets = route_match("/assets/*path", "/assets/css/app.css")

print(user["params"]["id"])
print(assets["params"]["path"])
```

## Project Layout

- `cmd/vibelang`: CLI entrypoint.
- `internal/lexer`: indentation-aware line lexer and tokenizer.
- `internal/parser`: AST builder for statements, expressions, and raw AI function bodies.
- `internal/runtime`: evaluator, builtins, type coercion, prompt construction, and AI tool-call loop.
- `internal/model`: Ollama and `llama.cpp` HTTP clients.
- `std`: bundled vibelang modules that ship prompt-native library helpers.
- `examples`: runnable sample programs.
- `docs`: tutorial, how-to, reference, and explanation documents.

## Expanded Standard Library

The deterministic runtime now covers more of the boring work that AI functions should not hallucinate:

- Filesystem: `read_file`, `write_file`, `append_file`, `copy_file`, `move_file`, `glob`, `read_json`, `write_json`
- Data: `json_parse`, `json_pretty`, `set`, `set_add`, `set_remove`, `set_has`, `set_values`, `set_union`, `set_intersection`, `set_difference`, `dict_has`, `dict_get`, `dict_set`, `dict_merge`, `sorted`, `unique`, `sum`, `min`, `max`
- Paths and strings: `join_path`, `abs_path`, `dirname`, `basename`, `split`, `join`, `replace`, `contains`, `base64_encode`, `base64_decode`, `url_encode`, `url_decode`, `query_encode`, `query_decode`, `url_parse`, `url_build`, `sha256`, `regex_match`, `regex_find_all`, `regex_replace`
- System: `run_process`, `env`, `cwd`, `now`, `unix_time`, `sleep`
- Math: `sqrt`, `pow`, `abs`, `floor`, `ceil`, plus `pi` and `e`
- Network: `http_request`, `http_request_json`, `socket_listen`, `socket_accept`, `socket_open`, `socket_write`, `socket_read`, `socket_local_addr`, `socket_remote_addr`, `socket_listener_close`, `socket_close`
- Concurrency: `spawn`, `await_task`, `task_status`, `channel`, `channel_send`, `channel_recv`, `channel_select`, `channel_close`, `mutex`, `mutex_lock`, `mutex_unlock`, `wait_group`, `wait_group_add`, `wait_group_done`, `wait_group_wait`
- Services: `route_match`, `http_serve`, `http_serve_routes`, `http_server_stop`
- Observability: `log`, `otel_init_stdout`, `otel_span_start`, `otel_span_event`, `otel_span_end`, `otel_flush`, `metrics_snapshot`

Bundled `std` modules currently include:

- `std/web`: AI helpers for HTML page rendering, component fragments, app shells, typed HTML responses, and JSON response construction via `respond_app_shell` and `respond_json`
- `std/telemetry`: AI helpers for summarizing runtime metrics

## Documentation

- [Tutorial](docs/tutorial.md)
- [How-to Guide](docs/how-to-run-local-models.md)
- [Reference](docs/reference.md)
- [Explanation](docs/explanation.md)

## Status

The interpreter is production-shaped, but the runtime behavior still depends on how well the selected local model follows the JSON protocol. Lower temperatures and smaller helper-call limits generally make execution more predictable. `run_process`, network access, and file-mutating helpers are intentionally powerful, so treat `.vibe` programs the way you would treat any other local code execution surface.
