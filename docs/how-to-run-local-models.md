# How To Run vibelang With Local Models

This guide is for developers who already understand what `vibelang` is and want a reliable way to connect it to a local model server.

## Run With Ollama

Ollama serves its local API at `http://localhost:11434/api` by default. `vibelang` uses the non-streaming `POST /api/generate` endpoint in JSON mode.

Start Ollama:

```bash
ollama serve
```

Pull the model you want:

```bash
ollama pull gemma4
```

Build and run the interpreter:

```bash
go build -o bin/vibelang ./cmd/vibelang
./bin/vibelang --provider ollama --model gemma4 examples/hello.vibe
```

Useful flags:

- `--endpoint`: override the default Ollama URL.
- `--temperature`: lower values make JSON output more stable.
- `--max-tokens`: cap the model output size for each AI step.
- `--trace`: inspect raw model responses and helper calls.

`examples/pi_file.vibe` is a good smoke test because it exercises inline prompts, boolean coercion, and filesystem tool calls.

## Run With llama.cpp

`vibelang` expects a running `llama-server`. It tries the native `/completion` endpoint first and falls back to the OpenAI-compatible `/v1/chat/completions` route when needed.

Start the server:

```bash
llama-server -m /models/gemma4.gguf --port 8080
```

Run a program:

```bash
./bin/vibelang \
  --provider llamacpp \
  --endpoint http://127.0.0.1:8080 \
  --model gemma4 \
  examples/hello.vibe
```

If your server is fronted by another host or port, point `--endpoint` at that address.

## Keep AI Execution Predictable

These settings usually help:

- keep `--temperature` near `0.1` to `0.2`
- keep `--max-steps` small unless helper chains are essential
- keep function instructions specific about shape and type
- declare return types instead of leaving them as `any`

## Troubleshoot Common Failures

### The model returns non-JSON text

- lower `--temperature`
- rerun with `--trace`
- tighten the function prompt so it asks for one specific output shape

### The model hallucinates helper names

- rename helper functions to short, concrete names
- prefer builtin tool names exactly as documented, such as `file_exists` or `write_file`
- make the caller body explicitly name the helper it should use
- reduce the number of in-scope helper functions if the program is large

### The returned value has the wrong type

- declare parameter and return types
- tell the model exactly what the return value should look like
- prefer smaller, single-purpose functions over broad prompts
