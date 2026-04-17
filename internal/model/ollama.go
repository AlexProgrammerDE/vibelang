package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ollamaClient struct {
	config     Config
	httpClient *http.Client
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   any             `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

type ollamaGenerateRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Format  any            `json:"format,omitempty"`
	Raw     bool           `json:"raw,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

func newOllamaClient(config Config) Client {
	return &ollamaClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

func (c *ollamaClient) Generate(ctx context.Context, request Request) (Response, error) {
	text, err := c.generateChat(ctx, request)
	if err == nil {
		return Response{Text: text}, nil
	}

	fallbackText, fallbackErr := c.generateLegacyPrompt(ctx, request)
	if fallbackErr == nil {
		return Response{Text: fallbackText}, nil
	}

	return Response{}, fmt.Errorf("ollama chat request failed: %v; generate fallback failed: %v", err, fallbackErr)
}

func (c *ollamaClient) generateChat(ctx context.Context, request Request) (string, error) {
	messages := make([]ollamaMessage, 0, 2)
	if strings.TrimSpace(request.System) != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: request.System})
	}
	messages = append(messages, ollamaMessage{Role: "user", Content: request.Prompt})

	payload := ollamaChatRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
		Format:   ollamaFormat(request.JSONSchema),
		Options: map[string]any{
			"temperature": c.config.Temperature,
			"num_predict": c.config.MaxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal ollama chat request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/api/chat"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build ollama chat request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("ollama chat request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return "", fmt.Errorf("read ollama chat response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return "", fmt.Errorf("ollama chat returned %s: %s", httpResponse.Status, strings.TrimSpace(string(responseBody)))
	}

	var response ollamaChatResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode ollama chat response: %w", err)
	}
	if response.Error != "" {
		return "", fmt.Errorf("ollama error: %s", response.Error)
	}
	if strings.TrimSpace(response.Message.Content) == "" {
		return "", fmt.Errorf("ollama chat returned empty content")
	}

	return response.Message.Content, nil
}

func (c *ollamaClient) generateLegacyPrompt(ctx context.Context, request Request) (string, error) {
	prompt := request.Prompt
	if strings.TrimSpace(request.System) != "" {
		prompt = request.System + "\n\n" + request.Prompt
	}

	payload := ollamaGenerateRequest{
		Model:  c.config.Model,
		Prompt: prompt,
		Stream: false,
		Format: ollamaFormat(request.JSONSchema),
		Raw:    true,
		Options: map[string]any{
			"temperature": c.config.Temperature,
			"num_predict": c.config.MaxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal ollama generate request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/api/generate"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build ollama generate request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("ollama generate request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return "", fmt.Errorf("read ollama generate response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return "", fmt.Errorf("ollama generate returned %s: %s", httpResponse.Status, strings.TrimSpace(string(responseBody)))
	}

	var response ollamaGenerateResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode ollama generate response: %w", err)
	}
	if response.Error != "" {
		return "", fmt.Errorf("ollama error: %s", response.Error)
	}
	if strings.TrimSpace(response.Response) == "" {
		return "", fmt.Errorf("ollama generate returned empty content")
	}

	return response.Response, nil
}

func ollamaFormat(schema map[string]any) any {
	if len(schema) == 0 {
		return "json"
	}
	return schema
}
