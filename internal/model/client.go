package model

import (
	"fmt"
	"strings"
)

func NewClient(config Config) (Client, error) {
	config = config.WithDefaults()

	switch strings.ToLower(config.Provider) {
	case "ollama":
		return newOllamaClient(config), nil
	case "llamacpp":
		return newLlamaCPPClient(config), nil
	case "openai", "groq", "openai-compatible":
		return newOpenAICompatibleClient(config)
	default:
		return nil, fmt.Errorf("unknown model provider %q", config.Provider)
	}
}
