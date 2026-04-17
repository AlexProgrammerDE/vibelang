# Explanation

## Why vibelang Exists

`vibelang` is built around a simple split:

- deterministic code handles structure
- the model handles intent inside functions and inline prompts

That makes it possible to keep the surrounding program legible and testable while still letting the model interpret plain-language function bodies.

## Why Function Bodies Are Raw Text

Most prompt-first systems force natural language into comments, strings, or special wrappers. `vibelang` makes the prompt the function body itself. That keeps the source readable and avoids inventing a second prompt DSL.

The tradeoff is that function bodies are not statically analyzable in the same way normal code is. The interpreter compensates by keeping everything around them strict:

- indentation-sensitive parsing
- normal expressions and control flow
- declared parameter and return types
- a JSON-only model protocol

Prompt interpolation stays on the deterministic side of the language. `${...}` placeholders are parsed as real vibelang expressions, so a prompt can reference `${items[0]}`, `${len(digits)}`, or `${basename(path)}` without forcing the model to reconstruct obvious facts from raw JSON inputs.

Inline `* prompt` expressions take the same idea one step further. They let you ask the model for a value or action directly inside an assignment, condition, loop header, or standalone statement, while still routing everything through the interpreter's normal execution model.

## Why Tool Calls Use a Runtime Loop

Instead of depending on provider-specific tool-calling features, `vibelang` asks the model to emit one JSON object per step. That keeps the execution model portable across Ollama and `llama.cpp`.

The loop looks like this:

1. build a prompt from the function body, bound inputs, helper list, and prior tool results
2. ask the local model for JSON
3. either return a final value or call one helper
4. repeat until a return arrives or the safety limits are hit

This design is less flashy than native provider tool calling, but it is easier to reason about and test locally.

## Why the Interpreter Is Still Useful

Without the interpreter, a prompt program quickly turns into opaque glue code. The interpreter adds structure that the model alone does not provide:

- lexical scoping for names
- collection literals and indexing
- loops and conditionals
- builtins for routine data work
- builtin tools for file access, path handling, JSON, strings, and environment inspection
- type coercion for model outputs
- bounded helper-call recursion

In practice that means you can keep AI functions focused on language-heavy tasks and leave the boring control flow to normal code.

## Limits

`vibelang` is intentionally opinionated.

- User-defined functions are AI-backed only.
- The model must follow a JSON protocol or execution fails.
- Helper calls are one at a time.
- Behavior quality still depends on the local model you run.

Those limits are deliberate. They keep the language small, the runtime understandable, and the failure modes visible.
