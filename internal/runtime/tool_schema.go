package runtime

import "vibelang/internal/ast"

func buildToolCallSchema(tools []ToolSpec) (map[string]any, error) {
	variants := make([]any, 0, len(tools))
	for _, tool := range tools {
		arguments, err := buildToolArgumentSchema(tool.Params)
		if err != nil {
			return nil, err
		}
		variants = append(variants, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":  "string",
					"const": tool.Name,
				},
				"arguments": arguments,
			},
			"required":             []string{"name", "arguments"},
			"additionalProperties": false,
		})
	}

	if len(variants) == 1 {
		if schema, ok := variants[0].(map[string]any); ok {
			return schema, nil
		}
	}

	return map[string]any{"oneOf": variants}, nil
}

func buildToolArgumentSchema(params []ast.Param) (map[string]any, error) {
	properties := make(map[string]any, len(params))
	required := make([]string, 0, len(params))

	for _, param := range params {
		spec, err := parseTypeSpec(param.Type.String())
		if err != nil {
			return nil, err
		}
		properties[param.Name] = spec.jsonSchema()
		if param.DefaultText == "" {
			required = append(required, param.Name)
		}
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, nil
}
