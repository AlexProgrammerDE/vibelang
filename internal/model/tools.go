package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

type providerToolDefinition struct {
	Type     string               `json:"type"`
	Function providerToolFunction `json:"function"`
}

type providerToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type providerToolCall struct {
	Type     string                   `json:"type,omitempty"`
	Function providerToolCallFunction `json:"function"`
}

type providerToolCallFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Arguments   any    `json:"arguments"`
}

func requestTools(request Request) []providerToolDefinition {
	if len(request.Tools) == 0 {
		return nil
	}

	tools := make([]providerToolDefinition, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, providerToolDefinition{
			Type: "function",
			Function: providerToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return tools
}

func parseToolCalls(toolCalls []providerToolCall) ([]ToolCall, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	result := make([]ToolCall, 0, len(toolCalls))
	for _, call := range toolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("tool call was missing a function name")
		}

		arguments, err := parseToolArguments(call.Function.Arguments)
		if err != nil {
			return nil, fmt.Errorf("tool call %s arguments: %w", name, err)
		}

		result = append(result, ToolCall{
			Name:      name,
			Arguments: arguments,
		})
	}
	return result, nil
}

func parseToolArguments(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return map[string]any{}, nil
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return nil, fmt.Errorf("decode JSON string: %w", err)
		}
		if decoded == nil {
			return map[string]any{}, nil
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported type %T", value)
	}
}
