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

type compiledPrompt struct {
	segments []promptSegment
}

type promptSegment struct {
	Literal string
	Source  string
	Expr    ast.Expr
}

func aiSystemPrompt() string {
	return strings.Join([]string{
		"You are the execution engine for vibelang.",
		"vibelang functions are defined as natural-language instructions.",
		"Always reply with a single JSON object that matches the provided execution schema.",
		"Use action=call only when exactly one helper function should run next.",
		"Prefer helper calls for deterministic filesystem, path, JSON, string, and environment work.",
		"Never use markdown, code fences, or extra commentary.",
	}, "\n")
}

func aiActionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"return", "call"},
			},
			"value": map[string]any{},
			"call": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
					"arguments": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
				"required":             []string{"name", "arguments"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"action"},
		"additionalProperties": true,
	}
}

func buildPrompt(function *AIFunction, instructions string, args map[string]any, tools []ToolSpec, history []ToolEvent) (string, error) {
	inputJSON, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal function inputs: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("Execute the following vibelang task.\n\n")

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

	builder.WriteString("Execution schema:\n")
	builder.WriteString(indentLines(jsonString(aiActionSchema()), "  "))
	builder.WriteString("\n\n")

	builder.WriteString("Return JSON only. Keep helper arguments valid for the declared parameter names.\n")
	builder.WriteString(fmt.Sprintf("The final value must match the declared return type %q.\n", function.Def.ReturnType.String()))

	return builder.String(), nil
}

func (i *Interpreter) renderPromptText(ctx context.Context, body string, values map[string]any) (string, error) {
	env := i.newPromptEnvironment(values)
	template, err := i.compilePrompt(body)
	if err != nil {
		return "", err
	}
	return template.render(func(segment promptSegment) (any, error) {
		return i.evaluateExpression(ctx, env, segment.Expr)
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
	if value, err := i.globals.Get("pi"); err == nil {
		env.Define("pi", value)
	}
	if value, err := i.globals.Get("e"); err == nil {
		env.Define("e", value)
	}
	for name, value := range values {
		env.Define(name, value)
	}
	return env
}

func (i *Interpreter) compilePrompt(body string) (*compiledPrompt, error) {
	i.mu.RLock()
	if cached, ok := i.promptCache[body]; ok {
		i.mu.RUnlock()
		return cached, nil
	}
	i.mu.RUnlock()

	template, err := parsePromptTemplate(body)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if cached, ok := i.promptCache[body]; ok {
		return cached, nil
	}
	i.promptCache[body] = template
	return template, nil
}

func parsePromptTemplate(body string) (*compiledPrompt, error) {
	segments := make([]promptSegment, 0)
	start := 0

	for index := 0; index < len(body); {
		if body[index] != '$' || index+1 >= len(body) || body[index+1] != '{' {
			index++
			continue
		}

		if start < index {
			segments = append(segments, promptSegment{Literal: body[start:index]})
		}

		exprSource, nextIndex, err := readPromptPlaceholder(body, index+2)
		if err != nil {
			return nil, err
		}
		exprSource = strings.TrimSpace(exprSource)
		if exprSource == "" {
			return nil, fmt.Errorf("prompt interpolation cannot be empty")
		}

		expr, err := parser.ParseExpressionSource(exprSource)
		if err != nil {
			return nil, fmt.Errorf("interpolate %q: %w", exprSource, err)
		}
		segments = append(segments, promptSegment{
			Source: exprSource,
			Expr:   expr,
		})

		index = nextIndex
		start = nextIndex
	}

	if start < len(body) {
		segments = append(segments, promptSegment{Literal: body[start:]})
	}
	if len(segments) == 0 {
		segments = append(segments, promptSegment{Literal: body})
	}

	return &compiledPrompt{segments: segments}, nil
}

func (p *compiledPrompt) render(resolve func(promptSegment) (any, error)) (string, error) {
	var builder strings.Builder

	for _, segment := range p.segments {
		if segment.Expr == nil {
			builder.WriteString(segment.Literal)
			continue
		}

		value, err := resolve(segment)
		if err != nil {
			return "", fmt.Errorf("interpolate %q: %w", segment.Source, err)
		}
		builder.WriteString(stringify(value))
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
		part := param.Name
		if param.Type.Expr != "" {
			part = fmt.Sprintf("%s: %s", part, param.Type.String())
		}
		if param.DefaultText != "" {
			part = fmt.Sprintf("%s = %s", part, param.DefaultText)
		}
		parts = append(parts, part)
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
