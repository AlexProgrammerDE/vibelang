package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

type aiDirectiveConfig struct {
	Temperature *float64
	MaxTokens   *int
	MaxSteps    *int
	AllowTools  map[string]struct{}
	DenyTools   map[string]struct{}
}

func parseAIBody(raw string) (aiDirectiveConfig, string, error) {
	lines := strings.Split(raw, "\n")
	config := aiDirectiveConfig{}
	bodyStart := 0

	for bodyStart < len(lines) {
		line := strings.TrimSpace(lines[bodyStart])
		if line == "" {
			bodyStart++
			continue
		}
		if !strings.HasPrefix(line, "@") {
			break
		}
		if err := applyAIDirective(&config, line, bodyStart+1); err != nil {
			return aiDirectiveConfig{}, "", err
		}
		bodyStart++
	}

	instructions := strings.TrimLeft(strings.Join(lines[bodyStart:], "\n"), "\n")
	if strings.TrimSpace(instructions) == "" {
		return aiDirectiveConfig{}, "", fmt.Errorf("AI body cannot contain only directives")
	}
	return config, instructions, nil
}

func applyAIDirective(config *aiDirectiveConfig, line string, lineNumber int) error {
	name, value, ok := splitDirective(line)
	if !ok {
		return fmt.Errorf("directive line %d must be in the form @name value", lineNumber)
	}

	switch name {
	case "temperature":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("directive line %d has invalid @temperature value %q", lineNumber, value)
		}
		config.Temperature = &parsed
		return nil
	case "max_tokens":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("directive line %d has invalid @max_tokens value %q", lineNumber, value)
		}
		config.MaxTokens = &parsed
		return nil
	case "max_steps":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("directive line %d has invalid @max_steps value %q", lineNumber, value)
		}
		config.MaxSteps = &parsed
		return nil
	case "tools":
		names, err := parseToolDirectiveNames(value, lineNumber, "@tools")
		if err != nil {
			return err
		}
		config.AllowTools = names
		return nil
	case "deny_tools":
		names, err := parseToolDirectiveNames(value, lineNumber, "@deny_tools")
		if err != nil {
			return err
		}
		config.DenyTools = names
		return nil
	default:
		return fmt.Errorf("directive line %d uses unknown AI directive @%s", lineNumber, name)
	}
}

func splitDirective(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "@") {
		return "", "", false
	}
	parts := strings.Fields(trimmed[1:])
	if len(parts) < 2 {
		return "", "", false
	}
	name := parts[0]
	value := strings.TrimSpace(trimmed[len(name)+2:])
	if value == "" {
		return "", "", false
	}
	return name, value, true
}

func parseToolDirectiveNames(value string, lineNumber int, directive string) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if !isDirectiveIdentifier(name) {
			return nil, fmt.Errorf("directive line %d has invalid tool name %q in %s", lineNumber, name, directive)
		}
		names[name] = struct{}{}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("directive line %d requires at least one tool name in %s", lineNumber, directive)
	}
	return names, nil
}

func isDirectiveIdentifier(value string) bool {
	for index, r := range value {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return value != ""
}

func (c aiDirectiveConfig) allowsTool(name string) bool {
	if len(c.AllowTools) > 0 {
		if _, ok := c.AllowTools[name]; !ok {
			return false
		}
	}
	if _, denied := c.DenyTools[name]; denied {
		return false
	}
	return true
}
