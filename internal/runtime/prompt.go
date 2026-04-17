package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vibelang/internal/ast"
	"vibelang/internal/parser"
)

type ToolEvent struct {
	Name      string
	Arguments map[string]any
	Result    any
}

func buildPrompt(function *AIFunction, instructions string, args map[string]any, tools []ToolSpec, history []ToolEvent) (string, error) {
	inputJSON, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal function inputs: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("You are the execution engine for vibelang.\n")
	builder.WriteString("vibelang functions are defined as natural-language instructions and must respond with JSON only.\n\n")

	builder.WriteString("Current function:\n")
	builder.WriteString(fmt.Sprintf("%s(%s) -> %s\n\n", function.Name(), formatParams(function.Def.Params), function.Def.ReturnType.String()))

	builder.WriteString("Function instructions:\n")
	builder.WriteString(instructions)
	builder.WriteString("\n\n")

	builder.WriteString("Inputs:\n")
	builder.Write(inputJSON)
	builder.WriteString("\n\n")

	if len(tools) == 0 {
		builder.WriteString("Available helper functions: none\n\n")
	} else {
		builder.WriteString("Available helper functions:\n")
		for _, tool := range tools {
			builder.WriteString(fmt.Sprintf("- %s(%s) -> %s\n", tool.Name, formatParams(tool.Params), tool.ReturnType.String()))
			builder.WriteString(indentLines(tool.Body, "  "))
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	if len(history) > 0 {
		builder.WriteString("Tool history:\n")
		for _, event := range history {
			builder.WriteString(fmt.Sprintf("- %s(%s) => %s\n", event.Name, jsonString(event.Arguments), jsonString(event.Result)))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("Return exactly one JSON object using one of these shapes:\n")
	builder.WriteString(`{"action":"return","value":<json>}` + "\n")
	builder.WriteString(`{"action":"call","call":{"name":"helper_name","arguments":{"param":"value"}}}` + "\n\n")

	builder.WriteString("Rules:\n")
	builder.WriteString("- Output JSON only. Do not use markdown.\n")
	builder.WriteString("- Use action=call only when one helper function is required next.\n")
	builder.WriteString("- Keep helper arguments valid for the declared parameter names.\n")
	builder.WriteString("- Prefer helper calls for deterministic filesystem, path, string, JSON, and environment work.\n")
	builder.WriteString(fmt.Sprintf("- The final value must match the declared return type %q.\n", function.Def.ReturnType.String()))

	return builder.String(), nil
}

func (i *Interpreter) renderPromptText(ctx context.Context, body string, values map[string]any) (string, error) {
	env := i.newPromptEnvironment(values)
	return interpolatePrompt(body, func(source string) (any, error) {
		expr, err := parser.ParseExpressionSource(source)
		if err != nil {
			return nil, err
		}
		return i.evaluateExpression(ctx, env, expr)
	})
}

func (i *Interpreter) newPromptEnvironment(values map[string]any) *Environment {
	env := NewEnvironment(nil)

	names := make([]string, 0, len(i.promptHelpers))
	for name := range i.promptHelpers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env.Define(name, i.promptHelpers[name])
	}
	for name, value := range values {
		env.Define(name, value)
	}
	return env
}

func interpolatePrompt(body string, resolve func(string) (any, error)) (string, error) {
	var builder strings.Builder

	for index := 0; index < len(body); {
		if body[index] == '$' && index+1 < len(body) && body[index+1] == '{' {
			exprSource, nextIndex, err := readPromptPlaceholder(body, index+2)
			if err != nil {
				return "", err
			}
			exprSource = strings.TrimSpace(exprSource)
			if exprSource == "" {
				return "", fmt.Errorf("prompt interpolation cannot be empty")
			}
			value, err := resolve(exprSource)
			if err != nil {
				return "", fmt.Errorf("interpolate %q: %w", exprSource, err)
			}
			builder.WriteString(stringify(value))
			index = nextIndex
			continue
		}

		builder.WriteByte(body[index])
		index++
	}

	return builder.String(), nil
}

func readPromptPlaceholder(body string, start int) (string, int, error) {
	depth := 1
	var quote byte
	escaped := false

	for index := start; index < len(body); index++ {
		ch := body[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}
			continue
		}

		switch ch {
		case '"', '\'':
			quote = ch
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start:index], index + 1, nil
			}
		}
	}

	return "", 0, fmt.Errorf("unterminated prompt interpolation")
}

func formatParams(params []ast.Param) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		if param.Type.Expr == "" {
			parts = append(parts, param.Name)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", param.Name, param.Type.String()))
	}
	return strings.Join(parts, ", ")
}

func indentLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func sortedToolSpecs(tools map[string]ToolCallable, current string) []ToolSpec {
	names := make([]string, 0, len(tools))
	for name := range tools {
		if name == current {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]ToolSpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, tools[name].ToolSpec())
	}
	return specs
}
