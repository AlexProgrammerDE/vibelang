package runtime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"vibelang/internal/ast"
)

type ToolEvent struct {
	Name      string
	Arguments map[string]any
	Result    any
}

var interpolationPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func buildPrompt(function *AIFunction, args map[string]any, tools []ToolSpec, history []ToolEvent) (string, error) {
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
	builder.WriteString(interpolatePrompt(function.Def.Body, args))
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
	builder.WriteString(fmt.Sprintf("- The final value must match the declared return type %q.\n", function.Def.ReturnType.String()))

	return builder.String(), nil
}

func interpolatePrompt(body string, args map[string]any) string {
	return interpolationPattern.ReplaceAllStringFunc(body, func(match string) string {
		name := interpolationPattern.FindStringSubmatch(match)[1]
		value, ok := args[name]
		if !ok {
			return match
		}
		switch value.(type) {
		case string, bool, int, int64, float64:
			return stringify(value)
		default:
			return jsonString(value)
		}
	})
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
