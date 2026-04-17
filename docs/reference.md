# Reference

## File Execution Model

- A `.vibe` file is parsed into an AST and executed top to bottom.
- `def` statements register AI-backed functions.
- `macro` statements register AI-backed expression macros.
- `import` statements load other `.vibe` files into isolated module scopes.
- Inline `* prompt` expressions execute AI work directly at the statement site.
- Regular statements are executed by the interpreter.
- AI function calls are delegated to the configured model client.

## Syntax

### Function Definition

```python
def name(param: type, other: type = "default") -> return_type:
    Plain-language instructions.
    These lines are preserved as raw text.
```

Notes:

- Function bodies are raw text, not statements.
- Leading lines that start with `@` are treated as AI directives before the remaining body text is sent to the model.
- `${...}` placeholders are evaluated as normal vibelang expressions before the prompt is sent to the model.
- Prompt interpolation can use arguments, current values, indexing, slicing, arithmetic, and prompt-safe builtins such as `len`, `json`, `basename`, or `join_path`.
- AI functions capture surrounding non-function values at definition time by value, so later list and dict mutations do not silently change prompt bodies.
- Parameter and return types are optional. Omitted types default to `any`.
- Parameters may declare default values. As in Python, required parameters must come before defaulted parameters.
- Structured record return types are supported with `dict{field: type, other: optional[list[string]]}`.
- Fixed-length tuples are supported with `tuple[T1, T2, ...]`.

### Macro Definition

```python
macro name(param: type) -> return_type:
    Return one valid vibelang expression for the desired expansion.
```

```python
result = @name(42)
```

Notes:

- Macro bodies are also raw text and are executed by the local model.
- Leading AI directives work in macros too.
- A macro must expand to exactly one valid vibelang expression source string.
- The expanded expression is then parsed and evaluated in the caller's environment.
- Macro prompts can use `${...}` interpolation just like AI functions.
- Macros can call helper functions through the same JSON tool-call loop as AI functions.

### Module Imports

```python
import "./shared.vibe" as shared
from "./shared.vibe" import format_name, helper as alias_helper
import "std/web" as web
import "github.com/example/vibelib/std/theme@main" as theme
```

Notes:

- Import paths are string literals.
- Relative paths resolve from the directory of the importing file.
- Bare imports are searched in the importing directory, `VIBE_PATH`, the current working directory, and the executable directory.
- `http://...` and `https://...` imports are fetched directly over HTTP.
- `github.com/owner/repo/path@ref` imports resolve to `raw.githubusercontent.com`.
- `import` binds a module namespace as a `dict`.
- Imported module namespaces support both `shared["name"]` and `shared.name`.
- `from ... import ...` binds exported names directly in the current scope.
- Any top-level name that starts with `_` stays private to the module.

### Inline Prompt Expression

```python
result = * return the first 5 digits of pi as a string.
```

```python
if * check whether ${path} exists:
    * delete the file at ${path}.
```

Notes:

- `* prompt` is a full expression form when it appears as the whole right-hand side of an assignment, the whole condition of `if`/`elif`/`while`, the iterable in `for ... in`, or a standalone expression statement.
- Inline prompts receive the current non-function variables as input.
- `${...}` interpolation also works inside inline prompts.
- Conditions coerce the returned value to `bool`. Other inline prompts default to `any`.

### AI Directives

Leading directive lines tune one AI body without changing global CLI flags:

```python
def slugify(title: string) -> string:
    @temperature 0
    @max_tokens 128
    @max_steps 4
    @cache true
    @tools lower, trim, replace, regex_replace
    Convert ${title} into a lowercase URL slug.
```

Supported directives:

- `@temperature <float>`: override sampling temperature for this function or macro
- `@max_tokens <int>`: override the per-step token budget
- `@max_steps <int>`: override the helper-call loop limit
- `@timeout_ms <int>`: override the backend request timeout used to construct the model client for this body
- `@cache <bool>`: opt into memoizing successful AI results for identical inputs within one interpreter run
- `@tools name_a, name_b`: allow only the listed helper functions
- `@deny_tools name_a, name_b`: hide specific helper functions from this body
- `@provider <name>`: route this body through a different provider such as `ollama`, `llamacpp`, `openai`, `groq`, or `openai-compatible`
- `@model <name>`: override the backend model name for this body
- `@endpoint <url>`: override the backend base URL for this body
- `@api_key_env <ENV_NAME>`: load a provider API key from one environment variable for this body

### Statements

Supported statements:

- macro definition: `macro name(param: type) -> type:`
- module import: `import "path" as name`, `from "path" import item`
- assignment: `name = expression`, `left, right = expression`
- index assignment: `items[0] = "updated"`
- member assignment: `config.name = "updated"`
- deferred cleanup: `defer expression`
- assertions: `assert condition`, `assert condition, "message"`
- expression statement: `print(value)`
- conditional: `if ...:`, `elif ...:`, `else:`
- pattern matching: `match subject:` with one or more `case pattern:` clauses
- loop: `while ...:` and `for target in iterable:`
- error handling: `try:`, `except err:`, `finally:`
- loop control: `break`, `continue`
- `pass`

### Try Statement

```python
try:
    fail("boom")
except err:
    print(err)
finally:
    print("cleanup complete")
```

Notes:

- `except` is optional when `finally` is present.
- `finally` is optional when `except` is present.
- `except` may bind the error text into one local name, for example `except err:`.
- Builtin failures, tool failures, and model/runtime errors raised inside the guarded block all flow through the same mechanism.

### Defer Statement

```python
path = join_path([cwd(), "notes.txt"])
defer delete_file(path)
write_file(path, "temporary")
```

Notes:

- `defer expression` registers one expression to run when the current block exits.
- Deferred expressions run in last-in, first-out order.
- They run on normal completion, `break`, `continue`, and errors.
- Deferred expressions capture the current visible non-function values at registration time, so later mutations do not silently change cleanup targets.
- A deferred expression error is reported after all remaining deferred expressions have run.

### Assert Statement

```python
assert len(items) > 0, "items should not be empty"
```

Notes:

- `assert` evaluates its condition using normal truthiness rules.
- When the condition is false and a message is present, the message expression is evaluated and included in the runtime error.
- `assert` is useful for deterministic invariants around AI outputs, tool results, and runtime checks.

### Match Statement

```python
match packet:
    case {"type": "ping"}:
        print("pong")
    case {"type": "message", "payload": [head, tail]} if head == tail:
        print("duplicate")
    case {"type": "message", "payload": [head, tail]}:
        print(head)
        print(tail)
    case _:
        print("fallback")
```

Notes:

- `match` evaluates the subject expression exactly once.
- Cases are checked from top to bottom until the first match succeeds.
- Supported patterns are literals, wildcard `_`, capture names, list patterns, and dict patterns.
- Capture names bind matched values into the current scope before the case body runs.
- Cases may add an `if` guard after the pattern. The guard runs only after the pattern matches, and a false guard falls through to the next case.
- Dict patterns require the referenced keys to exist and recursively match their values.

### Expressions

Supported expressions:

- identifiers
- macro expansion calls: `@build_value(arg1, name="Ada")`
- inline prompt expressions: `* do something with ${name}`
- literals: strings, integers, floats, `true`, `false`, `none`
- list literals: `[1, 2, 3]`
- list comprehensions: `[upper(name) for name in names if "a" in name]`
- dict literals: `{"name": "ada"}`
- dict comprehensions: `{name: len(name) for name in names if len(name) > 3}`
- arithmetic: `+`, `-`, `*`, `/`, `%`
- comparisons: `==`, `!=`, `<`, `<=`, `>`, `>=`, `in`
- boolean operators: `and`, `or`, `not`
- calls: `fn(arg1, arg2)` and `fn(name="Ada", tone="dry")`
- indexing: `items[0]`, `items[-1]`, `record["name"]`
- slicing: `items[1:3]`, `items[:3]`, `items[::2]`, `text[::-1]`
- member access: `shared.format_name`, `config.name`

Call notes:

- Keyword arguments must follow positional arguments.
- User-defined functions and eligible builtins both accept keyword arguments.
- Default parameter values are applied when arguments are omitted.
- `and` and `or` short-circuit and return the surviving operand value, matching Python-style truthiness.
- Slice bounds and steps use Python-like semantics for lists and strings, including negative indexes and omitted bounds.

## Types

Built-in type names:

- `any`
- `string`
- `int`
- `float`
- `bool`
- `none`
- `list`
- `set`
- `dict`
- `list[T]`
- `set[T]`
- `dict[T]`
- `dict[K, V]`
- `optional[T]`
- `oneof[T1, T2, ...]`
- `tuple[T1, T2, ...]`
- `dict{field: T, other: U}`

Notes:

- `dict{field: T}` defines a closed record shape. Unknown fields are rejected.
- Record fields declared as `optional[T]` may be omitted by the model and are normalized to `none`.
- `tuple[T1, T2, ...]` expects a fixed-length list-like value and coerces each slot independently.
- The runtime coerces model outputs to the declared return type when possible and derives a tighter JSON schema from the declared return type before calling the model.

## Builtins

- `print(...)`: write values to stdout
- `fail(message)`: raise a runtime error with a message
- `len(value)`: length of a string, list, or dict
- `str(value)`: convert to string
- `int(value)`: convert to integer
- `float(value)`: convert to float
- `bool(value)`: convert to boolean
- `type(value)`: return the runtime type name
- `tool_catalog(prefix="")`: return the available helper functions, optionally filtered by name prefix
- `tool_describe(name)`: return one helper function description with params, signature, return type, and body text
- `range(stop)` / `range(start, stop)` / `range(start, stop, step)`
- `append(list, value)`: return a new list with the appended value
- `keys(dict)`: return sorted dict keys
- `values(dict)`: return dict values in sorted-key order
- `json(value)`: JSON-encode a value
- `json_parse(text)`: parse JSON text into vibelang values
- `json_pretty(value, indent="  ")`: encode a value as indented JSON
- `yaml_parse(text)`: parse YAML text into vibelang values
- `yaml_stringify(value)`: encode a value as YAML
- `set(values)`: create a set from a list
- `set_values(set)`: return sorted set values as a list
- `set_has(set, value)`: membership check for sets
- `set_add(set, value)`: return a new set with one added value
- `set_remove(set, value)`: return a new set with one removed value
- `set_union(left, right)`: union of two sets
- `set_intersection(left, right)`: intersection of two sets
- `set_difference(left, right)`: difference of two sets
- `dict_has(dict, key)`: return whether a dict contains a key
- `dict_get(dict, key, default=none)`: fetch a dict value with a fallback
- `dict_items(dict)`: return sorted key/value entries as `{"key": ..., "value": ...}` dictionaries
- `dict_set(dict, key, value)`: return a new dict with one assignment applied
- `dict_merge(left, right)`: merge two dicts with right-hand keys winning
- `enumerate(values, start=0)`: return a list of `{"index": ..., "value": ...}` dictionaries
- `zip(left, right, strict=false)`: pair two lists into a list of two-item lists
- `sorted(values, descending=false)`: return a sorted copy of a list
- `unique(values)`: remove duplicates while preserving first occurrence order
- `sum(values)`: sum a list of numeric values
- `min(values)`: smallest value from a non-empty list
- `max(values)`: largest value from a non-empty list
- `cwd()`: return the current working directory
- `glob(pattern)`: return sorted matches for a glob pattern
- `file_exists(path)`: return whether a path exists
- `read_file(path)`: read a UTF-8 text file
- `join_path(parts)`: join path segments
- `abs_path(path)`: resolve an absolute path
- `dirname(path)`: return the parent directory
- `basename(path)`: return the final path element
- `list_dir(path)`: return sorted directory entries
- `is_dir(path)`: return whether a path is a directory
- `env(name)`: return an environment variable or `none`
- `lower(text)`: lowercase a string
- `upper(text)`: uppercase a string
- `trim(text)`: trim surrounding whitespace
- `split(text, separator)`: split a string into a list
- `join(values, separator)`: join a list into a string
- `replace(text, old, new)`: replace all substring matches
- `contains(container, value)`: containment check as a builtin helper
- `base64_encode(text)`: encode text as base64
- `base64_decode(text)`: decode base64 text
- `url_encode(text)`: percent-encode URL query text
- `url_decode(text)`: decode percent-encoded URL query text
- `query_encode(query)`: encode a dict of query parameters into a stable query string
- `query_decode(query)`: decode a query string into strings and lists
- `url_parse(raw_url)`: parse a URL into scheme, host, hostname, port, path, query, and fragment fields
- `url_build(parts)`: build a URL string from parsed parts
- `cookie_parse(header)`: parse a `Cookie` header into a dict of cookie values
- `cookie_build(name, value, attrs={})`: build one `Set-Cookie` header value; supported attrs include `path`, `domain`, `max_age`, `secure`, `http_only`, `same_site`, `expires`, and `partitioned`
- `html_escape(text)`: escape text for HTML
- `template_render(template, data)`: replace `${path}` placeholders from nested dict data
- `sha256(text)`: return the hex SHA-256 digest of a string
- `regex_match(pattern, text)`: test whether a regex matches
- `regex_find_all(pattern, text)`: return regex matches as a list
- `regex_replace(pattern, text, replacement)`: replace regex matches
- `read_json(path)`: read and decode JSON
- `read_yaml(path)`: read and decode YAML
- `write_file(path, content)`: write a UTF-8 text file and return the path
- `copy_file(source, destination)`: copy a file and return the destination path
- `move_file(source, destination)`: move or rename a file and return the destination path
- `delete_file(path)`: delete a file and return whether anything was removed
- `make_dir(path)`: create a directory tree and return the path
- `append_file(path, content)`: append text to a file and return the path
- `write_json(path, value)`: write formatted JSON and return the path
- `write_yaml(path, value)`: write YAML and return the path
- `sqrt(value)`: square root
- `pow(base, exponent)`: exponentiation helper
- `abs(value)`: absolute value
- `floor(value)`: floor as an integer
- `ceil(value)`: ceiling as an integer
- `now()`: current time in RFC3339 form
- `unix_time()`: current Unix timestamp
- `sleep(milliseconds)`: pause execution
- `http_request(url, method="GET", body="", headers={}, timeout_ms=10000)`: perform an HTTP request
- `http_request_json(url, method="GET", body=none, headers={}, timeout_ms=10000)`: send an optional JSON body and decode the JSON response into a `json` field
- `sse_event(data, event="message", id="", retry_ms=0)`: build one Server-Sent Event record
- `run_process(command, args=[], dir="", input="", env={}, timeout_ms=30000)`: execute a local process
- `socket_listen(address, network="tcp")`: start listening for socket connections and return `{handle, address}`
- `socket_accept(listener, timeout_ms=-1)`: accept the next connection and return `{ok, timeout, handle, local_addr, remote_addr}`
- `socket_open(address, network="tcp", timeout_ms=5000)`: open a socket and return a handle
- `socket_write(handle, data)`: write to an open socket
- `socket_read(handle, max_bytes=4096, timeout_ms=1000)`: read from an open socket
- `socket_local_addr(handle)`: return the local address for a socket handle
- `socket_remote_addr(handle)`: return the remote address for a socket handle
- `socket_listener_close(listener)`: close a socket listener handle
- `socket_close(handle)`: close an open socket
- `route_match(pattern, request_path)`: match a path against route patterns such as `/users/:id` or `/assets/*path` and return `{"matched": bool, "params": {...}}`
- `spawn(callable, args=[], kwargs={}, wait_group=none)`: run a function concurrently and return a task handle
- `await_task(task, timeout_ms=-1)`: wait for a task and return its result
- `task_status(task)`: inspect task completion, timestamps, result, or error
- `channel(capacity=0)`: create a channel handle
- `channel_send(channel, value, timeout_ms=-1)`: send a value to a channel
- `channel_recv(channel, timeout_ms=-1)`: receive a dict with `value`, `ok`, and `timeout`
- `channel_select(channels, timeout_ms=-1)`: wait on many channels and receive a dict with `channel`, `value`, `ok`, `closed`, and `timeout`
- `channel_close(channel)`: close a channel
- `mutex()`: create a mutex handle
- `mutex_lock(mutex, timeout_ms=-1)`: acquire a mutex
- `mutex_unlock(mutex)`: release a mutex
- `wait_group()`: create a wait group handle
- `wait_group_add(wait_group, delta=1)`: add to the counter and return the new value
- `wait_group_done(wait_group)`: decrement the counter and return the new value
- `wait_group_wait(wait_group, timeout_ms=-1)`: wait for the counter to reach zero
- `mime_type(path)`: guess the HTTP content type for a file path, including `application/wasm`
- `http_static_response(root, request, index_file="index.html", headers={}, cache_control="")`: serve one file from a static directory using `request["path"]`
- `http_serve(address, handler, read_timeout_ms=15000, write_timeout_ms=15000)`: start an HTTP server and return `{handle, address}`
- `http_serve_routes(address, routes, fallback=none, read_timeout_ms=15000, write_timeout_ms=15000)`: start an HTTP server backed by ordered route dicts containing `pattern`, `handler`, and optional `methods`
- `http_server_stop(handle, timeout_ms=5000)`: gracefully stop a server
- `log(message, level="info", fields={})`: emit one structured JSON log record to stderr
- `otel_init_stdout(service_name="vibelang", pretty=true)`: configure stdout OpenTelemetry tracing to stderr
- `otel_span_start(name, attributes={})`: start a span and return a handle
- `otel_span_event(span, name, attributes={})`: add an event to a span
- `otel_span_end(span, attributes={})`: finish a span
- `otel_flush()`: flush pending telemetry exports
- `metrics_snapshot()`: return interpreter counters such as AI requests, tool calls, tasks, and HTTP traffic
- `runtime_metrics()`: return a snapshot of Go runtime metrics such as `go.goroutine.count`, `go.memory.used`, and `go.memory.gc.goal`
- `runtime_metric(name, default=none)`: return one Go runtime metric by name, or the provided default when unavailable
- `cache_stats()`: return cache entry count together with cache-related metrics
- `cache_clear()`: clear cached AI results and return the number of removed entries
- `pi`, `e`: math constants exposed as top-level values

HTTP handler response notes:

- Plain values become `200 text/plain` responses.
- Response dicts may use exactly one of `body`, `html`, `json`, `sse`, or `sse_channel`.
- `sse` accepts one SSE event, a list of SSE events, or plain strings that become `data:` frames.
- `sse_channel` accepts a channel handle whose values are streamed as SSE frames until the channel closes or the client disconnects.
- `http_static_response` prevents directory traversal, serves directory indexes, and infers content types for frontend assets including `.wasm`.
- SSE responses default to `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-cache`, and `Connection: keep-alive`.

Bundled modules:

- `std/web`: AI helpers for HTML rendering, component fragments, app shells, typed HTML or JSON response construction, and SSE wrappers
- `std/telemetry`: AI helpers for summarizing runtime metrics
- `std/runtime`: AI helpers for summarizing live Go runtime snapshots
- `std/ai`: reusable AI helpers for rewriting, payload summaries, and release note drafting

## AI Function Protocol

Each AI-backed function is executed in a loop. The model must emit one JSON object in one of these forms:

```json
{"action":"return","value":42}
```

```json
{"action":"call","call":{"name":"helper_name","arguments":{"value":21}}}
```

Behavior:

- `return` ends the function.
- `call` invokes another user-defined function or a tool-capable builtin, records the result, and asks the model again.
- Helper calls may omit defaulted parameters, for example `{"action":"call","call":{"name":"range","arguments":{"stop":5}}}`.
- Helper call schemas are specialized per tool, so models see exact helper names plus the allowed and required argument fields for each callable.
- When a backend supports native tools, `vibelang` also sends the helper catalog through the provider `tools` field and accepts native `tool_calls` responses in addition to the JSON action envelope.
- Native `tool_calls` arrays are executed in order and each result is folded into the same tool history that the next model step sees.
- Per-body directives can lower temperature, shrink token budgets, lower step limits, or restrict which helpers the model can see.
- Per-body directives can also route a specific function or macro through a different backend endpoint, model, API-key env, or timeout without changing the global CLI flags.
- `@cache true` memoizes successful AI function and macro results for identical inputs and directives.
- Recursive helper re-entry through the active AI stack is rejected and fed back into the next model step through tool history.
- Repeating the same rejected helper call now fails fast instead of burning more model steps.
- Declared return types feed into the structured response schema, so `dict{...}`, `optional[...]`, and `tuple[...]` produce stricter model output contracts.
- Helper calls are limited by `--max-steps`.
- Nested AI execution is limited by `--max-depth`.

## AI Macro Protocol

Each AI-backed macro is executed in a loop. The model must emit one JSON object in one of these forms:

```json
{"action":"expand","source":"range(start=0, stop=10, step=2)"}
```

```json
{"action":"call","call":{"name":"json_parse","arguments":{"text":"{\"ok\":true}"}}}
```

Behavior:

- `expand` ends the macro and returns one expression source string.
- The interpreter parses that source as a normal expression and evaluates it in the caller environment.
- `call` invokes another user-defined function or a tool-capable builtin, records the result, and asks the model again.

## CLI

```bash
vibelang [flags] <file.vibe>
```

Flags:

- `--provider`: `ollama`, `llamacpp`, `openai`, `groq`, or `openai-compatible`
- `--endpoint`: model server URL
- `--model`: model name passed to the backend
- `--api-key`: API key for remote OpenAI-compatible providers
- `--temperature`: sampling temperature
- `--max-tokens`: max tokens per AI step
- `--max-steps`: max helper-call steps per AI function
- `--max-depth`: max nested AI call depth
- `--timeout`: HTTP timeout
- `--check`: parse only, do not execute the model
- `--trace`: print runtime trace to stderr
- `--version`: print the interpreter version

Environment variable equivalents:

- `VIBE_PROVIDER`
- `VIBE_ENDPOINT`
- `VIBE_MODEL`
- `VIBE_API_KEY`
- `VIBE_TEMPERATURE`
- `VIBE_MAX_TOKENS`
- `VIBE_MAX_STEPS`
- `VIBE_MAX_DEPTH`
- `VIBE_TIMEOUT`
- `VIBE_CHECK`
- `VIBE_TRACE`

Provider-specific API key environment variables:

- `OPENAI_API_KEY`
- `GROQ_API_KEY`
