# Execution Model

`vibelang` keeps deterministic orchestration in the interpreter and uses the model only for natural-language bodies and inline prompts.

## One AI Call

When an AI-backed function runs, the interpreter:

1. Binds arguments and default values.
2. Renders `${...}` placeholders with normal vibelang expression evaluation.
3. Builds a prompt containing:
   - the current function signature
   - the resolved natural-language instructions
   - the bound inputs as JSON
   - the active AI call stack
   - the visible helper/tool catalog
   - prior tool history for the current function call
4. Sends that prompt to the selected model backend with a strict JSON response schema.
5. Interprets the model result as one of:
   - `return`: finish the function with a value coerced to the declared return type
   - `call`: invoke one helper/tool and loop again
   - `call_many`: invoke a sequential batch of helpers/tools and loop again

The interpreter repeats that loop until it gets a valid return or hits the configured safety limits.

## Safety Limits

The runtime enforces limits at multiple layers:

- `--max-steps` or `@max_steps` bounds how many model turns one AI body may take.
- `--max-depth` bounds nested AI helper calls.
- Rejected helper calls are recorded in tool history.
- Repeating the same rejected helper call becomes a hard runtime error.
- Direct and indirect recursive AI re-entry is rejected before it can spiral into depth exhaustion.

These limits keep the runtime legible and make failures obvious instead of silently looping.

## System Prompting

Every AI call receives a fixed execution-system prompt that explains the JSON protocol and helper-call rules. `@system ...` appends body-local guidance to that base prompt instead of replacing it, so you can tighten behavior for one function or macro without dropping the interpreter's execution rules.

This is especially useful with Gemma 4 backends because Gemma 4 exposes native `system` role support in Ollama.

## Helper Calls

Helpers are ordinary builtins and AI functions exposed as structured tools.

- Deterministic helpers are preferred for filesystem, HTTP, parsing, text shaping, and concurrency work.
- AI functions may call other AI functions through the same tool loop.
- Tool-call history is fed back into later model turns so the model can see what already succeeded, failed, or was rejected.

Provider-native tool calls from Ollama, `llama.cpp`, and OpenAI-compatible APIs are normalized into the same runtime loop, so the interpreter behavior stays consistent even when transports differ.

## Model Routing

Process-wide flags configure the default backend, but one AI body can override that route:

- `@provider`
- `@model`
- `@endpoint`
- `@api_key_env`
- `@timeout_ms`

The interpreter caches routed model clients by effective config, so repeated calls through the same body-local route do not rebuild clients every time.

## Caching

`@cache true` memoizes successful AI results for the current interpreter run. The cache key includes:

- function identity
- rendered instructions
- bound inputs
- relevant directive settings

This keeps repeated deterministic AI helpers cheap without leaking cache state across runs.

## Deterministic Surface

The more work the deterministic runtime can do, the less work the model has to guess at. That is why the language includes builtins for:

- files and directories
- JSON, YAML, TOML, CSV, sets, dict/list helpers
- URLs, routes, Markdown rendering, cookies, and HTTP
- sockets, channels, mutexes, wait groups, and background tasks
- metrics and OpenTelemetry

The intended style is simple:

- keep structure and side effects deterministic
- keep AI prompts narrow and type-shaped
- use helper tools for boring work
- use the model for intent-heavy language work
