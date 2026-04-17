# vibelang

`vibelang` is a Python-shaped interpreted language where user-defined function bodies are plain language. The interpreter is written in Go, and every function call is executed by a local LLM running through Ollama or `llama.cpp`.

## What It Does

- Uses indentation-sensitive, Python-like syntax for variables, expressions, loops, and conditionals.
- Treats every `def` body as natural-language instructions instead of imperative code.
- Supports Python-style default parameter values and keyword arguments for user-defined functions and builtins.
- Supports inline `* prompt` expressions in assignments, conditions, loops, and standalone statements.
- Evaluates `${...}` prompt placeholders as real vibelang expressions, including indexing and prompt-safe builtins such as `len`, `basename`, or `join_path`.
- Lets AI functions call other AI functions through a strict JSON tool-call loop.
- Exposes a broader standard library for AI execution, including filesystem, path, JSON, string, and environment helpers.
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

## Project Layout

- `cmd/vibelang`: CLI entrypoint.
- `internal/lexer`: indentation-aware line lexer and tokenizer.
- `internal/parser`: AST builder for statements, expressions, and raw AI function bodies.
- `internal/runtime`: evaluator, builtins, type coercion, prompt construction, and AI tool-call loop.
- `internal/model`: Ollama and `llama.cpp` HTTP clients.
- `examples`: runnable sample programs.
- `docs`: tutorial, how-to, reference, and explanation documents.

## Documentation

- [Tutorial](docs/tutorial.md)
- [How-to Guide](docs/how-to-run-local-models.md)
- [Reference](docs/reference.md)
- [Explanation](docs/explanation.md)

## Status

The interpreter is production-shaped, but the runtime behavior still depends on how well the selected local model follows the JSON protocol. Lower temperatures and smaller helper-call limits generally make execution more predictable.
