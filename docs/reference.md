# Reference

## File Execution Model

- A `.vibe` file is parsed into an AST and executed top to bottom.
- `def` statements register AI-backed functions.
- Inline `* prompt` expressions execute AI work directly at the statement site.
- Regular statements are executed by the interpreter.
- AI function calls are delegated to the configured local model client.

## Syntax

### Function Definition

```python
def name(param: type, other: type = "default") -> return_type:
    Plain-language instructions.
    These lines are preserved as raw text.
```

Notes:

- Function bodies are raw text, not statements.
- `${...}` placeholders are evaluated as normal vibelang expressions before the prompt is sent to the model.
- Prompt interpolation can use arguments, current values, indexing, arithmetic, and prompt-safe builtins such as `len`, `json`, `basename`, or `join_path`.
- Parameter and return types are optional. Omitted types default to `any`.
- Parameters may declare default values. As in Python, required parameters must come before defaulted parameters.

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

### Statements

Supported statements:

- assignment: `name = expression`
- index assignment: `items[0] = "updated"`
- expression statement: `print(value)`
- conditional: `if ...:`, `elif ...:`, `else:`
- loop: `while ...:` and `for name in iterable:`
- loop control: `break`, `continue`
- `pass`

### Expressions

Supported expressions:

- identifiers
- inline prompt expressions: `* do something with ${name}`
- literals: strings, integers, floats, `true`, `false`, `none`
- list literals: `[1, 2, 3]`
- dict literals: `{"name": "ada"}`
- arithmetic: `+`, `-`, `*`, `/`, `%`
- comparisons: `==`, `!=`, `<`, `<=`, `>`, `>=`, `in`
- boolean operators: `and`, `or`, `not`
- calls: `fn(arg1, arg2)` and `fn(name="Ada", tone="dry")`
- indexing: `items[0]`, `record["name"]`

Call notes:

- Keyword arguments must follow positional arguments.
- User-defined functions and eligible builtins both accept keyword arguments.
- Default parameter values are applied when arguments are omitted.

## Types

Built-in type names:

- `any`
- `string`
- `int`
- `float`
- `bool`
- `none`
- `list`
- `dict`
- `list[T]`
- `dict[T]`
- `dict[K, V]`

The runtime coerces model outputs to the declared return type when possible.

## Builtins

- `print(...)`: write values to stdout
- `len(value)`: length of a string, list, or dict
- `str(value)`: convert to string
- `int(value)`: convert to integer
- `float(value)`: convert to float
- `bool(value)`: convert to boolean
- `type(value)`: return the runtime type name
- `range(stop)` / `range(start, stop)` / `range(start, stop, step)`
- `append(list, value)`: return a new list with the appended value
- `keys(dict)`: return sorted dict keys
- `values(dict)`: return dict values in sorted-key order
- `json(value)`: JSON-encode a value
- `cwd()`: return the current working directory
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
- `read_json(path)`: read and decode JSON
- `write_file(path, content)`: write a UTF-8 text file and return the path
- `delete_file(path)`: delete a file and return whether anything was removed
- `make_dir(path)`: create a directory tree and return the path
- `append_file(path, content)`: append text to a file and return the path
- `write_json(path, value)`: write formatted JSON and return the path

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
- Helper calls are limited by `--max-steps`.
- Nested AI execution is limited by `--max-depth`.

## CLI

```bash
vibelang [flags] <file.vibe>
```

Flags:

- `--provider`: `ollama` or `llamacpp`
- `--endpoint`: model server URL
- `--model`: model name passed to the backend
- `--temperature`: sampling temperature
- `--max-tokens`: max tokens per AI step
- `--max-steps`: max helper-call steps per AI function
- `--max-depth`: max nested AI call depth
- `--timeout`: HTTP timeout
- `--trace`: print runtime trace to stderr

Environment variable equivalents:

- `VIBE_PROVIDER`
- `VIBE_ENDPOINT`
- `VIBE_MODEL`
- `VIBE_TEMPERATURE`
- `VIBE_MAX_TOKENS`
- `VIBE_MAX_STEPS`
- `VIBE_MAX_DEPTH`
- `VIBE_TIMEOUT`
- `VIBE_TRACE`
