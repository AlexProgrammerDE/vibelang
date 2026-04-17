# Tutorial: Run Your First vibelang Program

This tutorial is for developers who want to get a first `vibelang` program running end to end with a local model.

## Goal

By the end, you will:

- start a local model server
- build the interpreter
- run a small `.vibe` program
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
def greet(name: string) -> string:
    Write a short, upbeat greeting for ${name}.
    Keep it to one sentence.

name = "Ada"
message = greet(name)
print(message)
```

The function body is plain text. `vibelang` passes the bound inputs to the model, asks for strict JSON, and then coerces the returned value to the declared type.

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

## 4. Run It

With Ollama:

```bash
./bin/vibelang --provider ollama --model gemma4 hello.vibe
```

With `llama.cpp`:

```bash
./bin/vibelang --provider llamacpp --endpoint http://127.0.0.1:8080 --model gemma4 hello.vibe
```

You should see a single generated line printed to stdout.

## 5. Turn On Tracing

Tracing is useful when the model decides to call helper functions or when it returns malformed JSON.

```bash
./bin/vibelang --provider ollama --model gemma4 --trace hello.vibe
```

The trace is written to stderr and includes raw model responses and helper-call activity.

## Next Steps

- Read the [how-to guide](how-to-run-local-models.md) for backend-specific setup.
- Read the [reference](reference.md) for language syntax and builtins.
- Run [examples/tool_chain.vibe](../examples/tool_chain.vibe) to see AI tool calls in action.
- Run [examples/pi_file.vibe](../examples/pi_file.vibe) to see inline prompts and filesystem tools together.
- Run [examples/stdlib.vibe](../examples/stdlib.vibe) to see expression-aware prompt interpolation plus the expanded standard library.
