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

type llamaCPPClient struct {
	config     Config
	httpClient *http.Client
}

func newLlamaCPPClient(config Config) Client {
	return &llamaCPPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

type llamaCompletionRequest struct {
	Prompt      string  `json:"prompt"`
	NPredict    int     `json:"n_predict"`
	Temperature float64 `json:"temperature"`
}

type llamaCompletionResponse struct {
	Content string `json:"content"`
}

type llamaChatRequest struct {
	Model       string         `json:"model,omitempty"`
	Messages    []llamaMessage `json:"messages"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float64        `json:"temperature"`
	Stream      bool           `json:"stream"`
}

type llamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llamaChatResponse struct {
	Choices []struct {
		Message llamaMessage `json:"message"`
	} `json:"choices"`
}

func (c *llamaCPPClient) Generate(ctx context.Context, request Request) (Response, error) {
	text, err := c.generateCompletion(ctx, request.Prompt)
	if err == nil {
		return Response{Text: text}, nil
	}

	fallbackText, fallbackErr := c.generateChatCompletion(ctx, request.Prompt)
	if fallbackErr == nil {
		return Response{Text: fallbackText}, nil
	}

	return Response{}, fmt.Errorf("llama.cpp request failed: %v; chat fallback failed: %v", err, fallbackErr)
}

func (c *llamaCPPClient) generateCompletion(ctx context.Context, prompt string) (string, error) {
	payload := llamaCompletionRequest{
		Prompt:      prompt,
		NPredict:    c.config.MaxTokens,
		Temperature: c.config.Temperature,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal llama.cpp completion request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/completion"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build llama.cpp completion request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("llama.cpp completion request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return "", fmt.Errorf("read llama.cpp completion response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return "", fmt.Errorf("llama.cpp completion returned %s: %s", httpResponse.Status, strings.TrimSpace(string(responseBody)))
	}

	var response llamaCompletionResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode llama.cpp completion response: %w", err)
	}
	if strings.TrimSpace(response.Content) == "" {
		return "", fmt.Errorf("llama.cpp completion returned empty content")
	}

	return response.Content, nil
}

func (c *llamaCPPClient) generateChatCompletion(ctx context.Context, prompt string) (string, error) {
	payload := llamaChatRequest{
		Model: c.config.Model,
		Messages: []llamaMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
		Stream:      false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal llama.cpp chat request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/v1/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build llama.cpp chat request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("llama.cpp chat request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return "", fmt.Errorf("read llama.cpp chat response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return "", fmt.Errorf("llama.cpp chat returned %s: %s", httpResponse.Status, strings.TrimSpace(string(responseBody)))
	}

	var response llamaChatResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode llama.cpp chat response: %w", err)
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("llama.cpp chat returned empty content")
	}

	return response.Choices[0].Message.Content, nil
}
