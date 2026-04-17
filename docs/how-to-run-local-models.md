# How To Run vibelang With Local Models

This guide is for developers who already understand what `vibelang` is and want a reliable way to connect it to a local model server.

## Run With Ollama

Ollama serves its local API at `http://localhost:11434/api` by default. `vibelang` uses the non-streaming `POST /api/chat` endpoint with structured JSON output, falls back to `POST /api/generate` when needed, sends tighter per-helper JSON schemas so models see exact helper argument contracts instead of one loose `arguments` object, and now also passes native provider `tools` definitions so Ollama can answer with `tool_calls` directly, including multi-call batches.

Start Ollama:

```bash
ollama serve
```

Pull the model you want:

```bash
ollama pull gemma4
```

If you want a lighter local model, `gemma4:e4b` is a good smaller tag to try.

Build and run the interpreter:

```bash
go build -o bin/vibelang ./cmd/vibelang
./bin/vibelang --provider ollama --model gemma4 examples/hello.vibe
```

Useful flags:

- `--endpoint`: override the default Ollama URL.
- `--temperature`: lower values make JSON output more stable.
- `--max-tokens`: cap the model output size for each AI step.
- `--check`: parse the program and exit before contacting the model.
- `--trace`: inspect raw model responses and helper calls.

`examples/pi_file.vibe` is a good smoke test because it exercises inline prompts, boolean coercion, and filesystem tool calls. `examples/modules/main.vibe` is useful once you want to verify module imports and captured prompt scope. `examples/slices.vibe` verifies the Python-style slicing surface without needing a model call. `examples/comprehensions.vibe` covers Python-style list and dict comprehensions. `examples/macros.vibe` covers AI macro expansion. `examples/observability.vibe` covers sets, JSON text helpers, structured logs, and OpenTelemetry tracing. `examples/defer.vibe` covers block-scoped cleanup. `examples/runtime_metrics.vibe` covers deterministic assertions and live Go runtime metrics. `examples/url_tools.vibe` covers deterministic URL parsing and query encoding. `examples/config_tools.vibe` covers TOML parsing, route construction, and Markdown rendering. `examples/yaml.vibe` covers YAML parsing and file IO. `examples/cookies.vibe` covers HTTP cookie helpers. `examples/routes.vibe` covers deterministic route matching for AI-backed servers. `examples/collections.vibe` covers Python-shaped collection helpers such as `all`, `any`, `reversed`, `flatten`, and `batched`. `examples/react_shell.vibe` shows the bundled `std/react` prompt module. `examples/stdlib.vibe`, `examples/ops.vibe`, and `examples/select.vibe` cover the broader deterministic standard library and channel coordination helpers.

## Run With llama.cpp

`vibelang` expects a running `llama-server`. It uses the OpenAI-compatible `/v1/chat/completions` route with `response_format` first, now sends provider-native `tools` definitions there as well, executes native `tool_calls` arrays when the server emits them, and falls back to the native `/completion` endpoint when needed.

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

## Optional: Run With Remote OpenAI-Compatible Providers

`vibelang` can also talk to remote OpenAI-compatible backends when you want to compare a hosted model against the local path.

With OpenAI:

```bash
export OPENAI_API_KEY=...
./bin/vibelang --provider openai --model gpt-4.1-mini examples/hello.vibe
```

With Groq:

```bash
export GROQ_API_KEY=...
./bin/vibelang --provider groq --model openai/gpt-oss-20b examples/hello.vibe
```

`vibelang` clamps a requested zero temperature to a tiny positive value for Groq so deterministic prompts still work with Groq's OpenAI-compatible API.

With another OpenAI-compatible gateway:

```bash
export VIBE_API_KEY=...
./bin/vibelang \
  --provider openai-compatible \
  --endpoint https://your-gateway.example.com \
  --model your-model \
  examples/hello.vibe
```

## Keep AI Execution Predictable

These settings usually help:

- keep `--temperature` near `0.1` to `0.2`
- keep `--max-steps` small unless helper chains are essential
- keep function instructions specific about shape and type
- use `@system` when one function needs stricter local steering without affecting the rest of the process
- declare return types instead of leaving them as `any`
- prefer explicit default parameters and keyword arguments when a function has optional inputs
- use `@cache true` for deterministic AI helpers that are expensive and likely to repeat in one run
- keep deterministic work in normal code or helpers, and let the model focus on intent-heavy text generation

## Troubleshoot Common Failures

### The model returns non-JSON text

- lower `--temperature`
- rerun with `--trace`
- tighten the function prompt so it asks for one specific output shape
- for macros, explicitly ask for "one valid vibelang expression" rather than prose

### The model hallucinates helper names

- rename helper functions to short, concrete names
- prefer builtin tool names exactly as documented, such as `file_exists` or `write_file`
- make the caller body explicitly name the helper it should use
- reduce the number of in-scope helper functions if the program is large

### The model keeps retrying a rejected helper call

- rerun with `--trace` and inspect the tool history section
- make the function body explicitly state which helper must not be called again
- split broad AI functions into smaller helper-oriented functions so the next step is obvious
- if the runtime now fails fast on a repeated rejected helper, treat that as a prompt design bug rather than increasing `--max-steps`

### The returned value has the wrong type

- declare parameter and return types
- tell the model exactly what the return value should look like
- prefer smaller, single-purpose functions over broad prompts
- for macros, make the prompt say whether the expansion should produce a `list`, `dict`, `string`, or another concrete type
