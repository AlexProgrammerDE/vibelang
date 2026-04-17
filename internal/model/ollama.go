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

type ollamaRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Format  string         `json:"format,omitempty"`
	Raw     bool           `json:"raw,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaResponse struct {
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
	payload := ollamaRequest{
		Model:  c.config.Model,
		Prompt: request.Prompt,
		Stream: false,
		Format: "json",
		Raw:    true,
		Options: map[string]any{
			"temperature": c.config.Temperature,
			"num_predict": c.config.MaxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("marshal ollama request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/api/generate"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build ollama request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return Response{}, fmt.Errorf("ollama request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read ollama response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return Response{}, fmt.Errorf("ollama returned %s: %s", httpResponse.Status, strings.TrimSpace(string(responseBody)))
	}

	var response ollamaResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return Response{}, fmt.Errorf("decode ollama response: %w", err)
	}
	if response.Error != "" {
		return Response{}, fmt.Errorf("ollama error: %s", response.Error)
	}

	return Response{Text: response.Response}, nil
}
