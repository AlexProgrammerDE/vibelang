package model

import (
	"context"
	"time"
)

type Config struct {
	Provider    string
	Endpoint    string
	Model       string
	APIKey      string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
}

type Request struct {
	System      string
	Prompt      string
	JSONSchema  map[string]any
	Temperature *float64
	MaxTokens   *int
}

type Response struct {
	Text string
}

type Client interface {
	Generate(ctx context.Context, request Request) (Response, error)
}

func (c Config) WithDefaults() Config {
	switch c.Provider {
	case "openai_compatible":
		c.Provider = "openai-compatible"
	}
	if c.Provider == "" {
		c.Provider = "ollama"
	}
	if c.Model == "" {
		c.Model = "gemma4"
	}
	if c.Temperature == 0 {
		c.Temperature = 0.2
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 768
	}
	if c.Timeout == 0 {
		c.Timeout = 2 * time.Minute
	}
	if c.Endpoint == "" {
		switch c.Provider {
		case "ollama":
			c.Endpoint = "http://127.0.0.1:11434"
		case "llamacpp":
			c.Endpoint = "http://127.0.0.1:8080"
		case "openai":
			c.Endpoint = "https://api.openai.com"
		case "groq":
			c.Endpoint = "https://api.groq.com/openai"
		}
	}
	return c
}
