package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

func macroSystemPrompt() string {
	return strings.Join([]string{
		"You are the macro expansion engine for vibelang.",
		"Macros must expand to one valid vibelang expression source string.",
		"Always reply with a single JSON object that matches the provided macro schema.",
		"Use action=call only when exactly one helper function should run next.",
		"Never use markdown, code fences, or extra commentary.",
	}, "\n")
}

func buildMacroActionSchema(tools []ToolSpec) (map[string]any, error) {
	variants := []any{
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":  "string",
					"const": "expand",
				},
				"source": map[string]any{
					"type": "string",
				},
			},
			"required":             []string{"action", "source"},
			"additionalProperties": false,
		},
	}

	if len(tools) > 0 {
		callSchema, err := buildToolCallSchema(tools)
		if err != nil {
			return nil, err
		}
		variants = append(variants, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":  "string",
					"const": "call",
				},
				"call": callSchema,
			},
			"required":             []string{"action", "call"},
			"additionalProperties": false,
		})
	}

	return map[string]any{"oneOf": variants}, nil
}

func buildMacroPrompt(macro *AIMacro, instructions string, args map[string]any, tools []ToolSpec, history []ToolEvent, schema map[string]any) (string, error) {
	inputJSON, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal macro inputs: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("Expand the following vibelang macro.\n\n")

	builder.WriteString("Current macro:\n")
	builder.WriteString(fmt.Sprintf("%s(%s) -> %s\n\n", macro.Name(), formatParams(macro.Def.Params), macro.Def.ReturnType.String()))

	builder.WriteString("Macro instructions:\n")
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
			switch {
			case event.Rejected:
				builder.WriteString(fmt.Sprintf("- %s(%s) => rejected: %s\n", event.Name, jsonString(event.Arguments), event.Error))
			case event.Error != "":
				builder.WriteString(fmt.Sprintf("- %s(%s) => error: %s\n", event.Name, jsonString(event.Arguments), event.Error))
			default:
				builder.WriteString(fmt.Sprintf("- %s(%s) => %s\n", event.Name, jsonString(event.Arguments), jsonString(event.Result)))
			}
		}
		builder.WriteString("\n")
	}

	builder.WriteString("Macro schema:\n")
	builder.WriteString(indentLines(jsonString(schema), "  "))
	builder.WriteString("\n\n")
	builder.WriteString("When finished, return action=expand and put exactly one valid vibelang expression in source.\n")
	builder.WriteString("Every helper call must use exactly the declared argument names and argument types.\n")
	builder.WriteString("Do not wrap the expression in markdown or explanations.\n")

	return builder.String(), nil
}
