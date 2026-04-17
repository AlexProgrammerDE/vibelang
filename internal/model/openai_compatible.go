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

type openAICompatibleClient struct {
	config     Config
	httpClient *http.Client
}

type openAIChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    float64         `json:"temperature"`
	Stream         bool            `json:"stream"`
	ResponseFormat map[string]any  `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openAIAPIError struct {
	StatusCode int
	Message    string
}

func (e *openAIAPIError) Error() string {
	return fmt.Sprintf("chat completions returned %d: %s", e.StatusCode, e.Message)
}

func newOpenAICompatibleClient(config Config) (Client, error) {
	if strings.TrimSpace(config.Endpoint) == "" {
		return nil, fmt.Errorf("%s requires an endpoint", config.Provider)
	}
	if config.Provider != "openai-compatible" && strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("%s requires an API key", config.Provider)
	}
	return &openAICompatibleClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

func (c *openAICompatibleClient) Generate(ctx context.Context, request Request) (Response, error) {
	text, err := c.generateChatCompletion(ctx, request, openAIResponseFormat(request.JSONSchema, true))
	if err == nil {
		return Response{Text: text}, nil
	}

	apiErr, ok := err.(*openAIAPIError)
	if ok && apiErr.StatusCode == http.StatusBadRequest && len(request.JSONSchema) > 0 {
		text, retryErr := c.generateChatCompletion(ctx, request, openAIResponseFormat(request.JSONSchema, false))
		if retryErr == nil {
			return Response{Text: text}, nil
		}
		return Response{}, retryErr
	}

	return Response{}, err
}

func (c *openAICompatibleClient) generateChatCompletion(ctx context.Context, request Request, responseFormat map[string]any) (string, error) {
	messages := make([]openAIMessage, 0, 2)
	if strings.TrimSpace(request.System) != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: request.System})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: request.Prompt})

	payload := openAIChatRequest{
		Model:          c.config.Model,
		Messages:       messages,
		MaxTokens:      c.maxTokens(request),
		Temperature:    c.temperature(request),
		Stream:         false,
		ResponseFormat: responseFormat,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.Endpoint, "/") + "/v1/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build chat request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.config.APIKey) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return "", fmt.Errorf("read chat response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		return "", &openAIAPIError{
			StatusCode: httpResponse.StatusCode,
			Message:    strings.TrimSpace(string(responseBody)),
		}
	}

	var response openAIChatResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("chat response error: %s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("chat response did not include any choices")
	}

	text, err := openAIMessageText(response.Choices[0].Message.Content)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("chat response returned empty content")
	}
	return text, nil
}

func openAIResponseFormat(schema map[string]any, preferSchema bool) map[string]any {
	if len(schema) == 0 || !preferSchema {
		return map[string]any{
			"type": "json_object",
		}
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "vibelang_action",
			"strict": false,
			"schema": schema,
		},
	}
}

func openAIMessageText(content any) (string, error) {
	switch value := content.(type) {
	case string:
		return value, nil
	case []any:
		var builder strings.Builder
		for _, item := range value {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				builder.WriteString(text)
			}
		}
		if builder.Len() == 0 {
			return "", fmt.Errorf("chat response did not include text content")
		}
		return builder.String(), nil
	default:
		return "", fmt.Errorf("chat response content had unsupported type %T", content)
	}
}

func (c *openAICompatibleClient) temperature(request Request) float64 {
	if request.Temperature != nil {
		return *request.Temperature
	}
	return c.config.Temperature
}

func (c *openAICompatibleClient) maxTokens(request Request) int {
	if request.MaxTokens != nil {
		return *request.MaxTokens
	}
	return c.config.MaxTokens
}
