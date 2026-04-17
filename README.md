# vibelang

`vibelang` is a Python-shaped interpreted language where user-defined function bodies are plain language. The interpreter is written in Go, and every function call is executed by a local LLM running through Ollama or `llama.cpp`.

## What It Does

- Uses indentation-sensitive, Python-like syntax for variables, expressions, loops, and conditionals.
- Treats every `def` body as natural-language instructions instead of imperative code.
- Supports module loading with `import "./module.vibe" as module` and `from "./module.vibe" import helper`.
- Supports Python-style default parameter values and keyword arguments for user-defined functions and builtins.
- Supports inline `* prompt` expressions in assignments, conditions, loops, and standalone statements.
- Evaluates `${...}` prompt placeholders as real vibelang expressions, including indexing and prompt-safe builtins such as `len`, `basename`, or `join_path`.
- Lets AI functions call other AI functions through a strict JSON tool-call loop.
- Captures surrounding non-function values when an AI function is defined, so prompts can safely use module constants and top-level configuration.
- Exposes a broader standard library for AI execution, including filesystem, path, JSON, string, environment, globbing, HTTP, TCP sockets, time, math, and local process helpers.
- Runs against local model servers, with first-class support for Ollama and `llama.cpp`.
- Sends chat-style structured JSON requests to local backends, which works better with modern Gemma 4 model servers.

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
print(shared["format_name"]("Grace"))
```

## Project Layout

- `cmd/vibelang`: CLI entrypoint.
- `internal/lexer`: indentation-aware line lexer and tokenizer.
- `internal/parser`: AST builder for statements, expressions, and raw AI function bodies.
- `internal/runtime`: evaluator, builtins, type coercion, prompt construction, and AI tool-call loop.
- `internal/model`: Ollama and `llama.cpp` HTTP clients.
- `examples`: runnable sample programs.
- `docs`: tutorial, how-to, reference, and explanation documents.

## Expanded Standard Library

The deterministic runtime now covers more of the boring work that AI functions should not hallucinate:

- Filesystem: `read_file`, `write_file`, `append_file`, `copy_file`, `move_file`, `glob`, `read_json`, `write_json`
- Paths and strings: `join_path`, `abs_path`, `dirname`, `basename`, `split`, `join`, `replace`, `contains`
- System: `run_process`, `env`, `cwd`, `now`, `unix_time`, `sleep`
- Math: `sqrt`, `pow`, `abs`, `floor`, `ceil`, plus `pi` and `e`
- Network: `http_request`, `socket_open`, `socket_write`, `socket_read`, `socket_close`

## Documentation

- [Tutorial](docs/tutorial.md)
- [How-to Guide](docs/how-to-run-local-models.md)
- [Reference](docs/reference.md)
- [Explanation](docs/explanation.md)

## Status

The interpreter is production-shaped, but the runtime behavior still depends on how well the selected local model follows the JSON protocol. Lower temperatures and smaller helper-call limits generally make execution more predictable. `run_process`, network access, and file-mutating helpers are intentionally powerful, so treat `.vibe` programs the way you would treat any other local code execution surface.
