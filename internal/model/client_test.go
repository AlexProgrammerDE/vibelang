package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaClientUsesChatAPIWithSchema(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"{\"action\":\"return\",\"value\":42}"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "ollama",
		Endpoint: server.URL,
		Model:    "gemma4",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
		JSONSchema: map[string]any{
			"type": "object",
		},
		Temperature: func() *float64 { value := 0.0; return &value }(),
		MaxTokens:   func() *int { value := 32; return &value }(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if response.Text != `{"action":"return","value":42}` {
		t.Fatalf("unexpected response text: %q", response.Text)
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 chat messages, got %#v", payload["messages"])
	}

	systemMessage := messages[0].(map[string]any)
	if systemMessage["role"] != "system" || systemMessage["content"] != "system prompt" {
		t.Fatalf("unexpected system message: %#v", systemMessage)
	}

	userMessage := messages[1].(map[string]any)
	if userMessage["role"] != "user" || userMessage["content"] != "user prompt" {
		t.Fatalf("unexpected user message: %#v", userMessage)
	}

	format, ok := payload["format"].(map[string]any)
	if !ok || format["type"] != "object" {
		t.Fatalf("expected JSON schema format, got %#v", payload["format"])
	}
	options, ok := payload["options"].(map[string]any)
	if !ok {
		t.Fatalf("expected options, got %#v", payload["options"])
	}
	if options["temperature"] != float64(0) || options["num_predict"] != float64(32) {
		t.Fatalf("expected request overrides in ollama payload, got %#v", options)
	}
}

func TestOllamaClientPreservesExplicitZeroTemperature(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"{\"action\":\"return\",\"value\":42}"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider:       "ollama",
		Endpoint:       server.URL,
		Model:          "gemma4",
		Temperature:    0,
		HasTemperature: true,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if _, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	options, ok := payload["options"].(map[string]any)
	if !ok {
		t.Fatalf("expected options, got %#v", payload["options"])
	}
	if options["temperature"] != float64(0) {
		t.Fatalf("expected explicit zero temperature, got %#v", options["temperature"])
	}
}

func TestLlamaCPPClientUsesChatCompletionsResponseFormat(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"return\",\"value\":true}"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "llamacpp",
		Endpoint: server.URL,
		Model:    "gemma4",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
		JSONSchema: map[string]any{
			"type": "object",
		},
		Temperature: func() *float64 { value := 0.0; return &value }(),
		MaxTokens:   func() *int { value := 48; return &value }(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if response.Text != `{"action":"return","value":true}` {
		t.Fatalf("unexpected response text: %q", response.Text)
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 chat messages, got %#v", payload["messages"])
	}

	responseFormat, ok := payload["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("expected response_format, got %#v", payload["response_format"])
	}
	if responseFormat["type"] != "json_object" {
		t.Fatalf("unexpected response_format type: %#v", responseFormat)
	}
	if _, ok := responseFormat["schema"].(map[string]any); !ok {
		t.Fatalf("expected response_format schema, got %#v", responseFormat)
	}
	if payload["temperature"] != float64(0) || payload["max_tokens"] != float64(48) {
		t.Fatalf("expected request overrides in llama.cpp payload, got %#v", payload)
	}
}

func TestOpenAICompatibleClientUsesChatCompletionsWithSchema(t *testing.T) {
	var payload map[string]any
	authHeader := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"return\",\"value\":\"ok\"}"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "openai-compatible",
		Endpoint: server.URL,
		Model:    "demo-model",
		APIKey:   "secret-token",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
		JSONSchema: map[string]any{
			"type": "object",
		},
		Temperature: func() *float64 { value := 0.0; return &value }(),
		MaxTokens:   func() *int { value := 24; return &value }(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if response.Text != `{"action":"return","value":"ok"}` {
		t.Fatalf("unexpected response text: %q", response.Text)
	}
	if authHeader != "Bearer secret-token" {
		t.Fatalf("unexpected auth header: %q", authHeader)
	}

	responseFormat, ok := payload["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("expected response_format, got %#v", payload["response_format"])
	}
	if responseFormat["type"] != "json_schema" {
		t.Fatalf("unexpected response_format type: %#v", responseFormat)
	}
	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected json_schema config, got %#v", responseFormat["json_schema"])
	}
	if jsonSchema["name"] != "vibelang_action" {
		t.Fatalf("unexpected schema name: %#v", jsonSchema["name"])
	}
	if _, ok := jsonSchema["schema"].(map[string]any); !ok {
		t.Fatalf("expected schema payload, got %#v", jsonSchema["schema"])
	}
	if payload["temperature"] != float64(0) || payload["max_tokens"] != float64(24) {
		t.Fatalf("expected request overrides in openai-compatible payload, got %#v", payload)
	}
}

func TestOllamaClientReturnsNativeToolCalls(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"","tool_calls":[{"function":{"name":"lookup_weather","arguments":{"city":"Berlin"}}}]}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "ollama",
		Endpoint: server.URL,
		Model:    "gemma4",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
		Tools: []ToolDefinition{
			{
				Name:        "lookup_weather",
				Description: "Look up weather for a city.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if len(response.ToolCalls) != 1 {
		t.Fatalf("expected 1 native tool call response, got %#v", response)
	}
	if response.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("unexpected tool name: %#v", response.ToolCalls)
	}
	if response.ToolCalls[0].Arguments["city"] != "Berlin" {
		t.Fatalf("unexpected tool arguments: %#v", response.ToolCalls[0].Arguments)
	}
	if _, ok := payload["tools"].([]any); !ok {
		t.Fatalf("expected tools in ollama payload, got %#v", payload["tools"])
	}
}

func TestOllamaClientReturnsAllNativeToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"","tool_calls":[{"function":{"name":"lookup_weather","arguments":{"city":"Berlin"}}},{"function":{"name":"lookup_timezone","arguments":{"city":"Berlin"}}}]}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "ollama",
		Endpoint: server.URL,
		Model:    "gemma4",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		Prompt: "user prompt",
		Tools: []ToolDefinition{
			{
				Name:        "lookup_weather",
				Description: "Look up weather for a city.",
				Parameters:  map[string]any{"type": "object"},
			},
			{
				Name:        "lookup_timezone",
				Description: "Look up timezone for a city.",
				Parameters:  map[string]any{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if len(response.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %#v", response.ToolCalls)
	}
	if response.ToolCalls[0].Name != "lookup_weather" || response.ToolCalls[1].Name != "lookup_timezone" {
		t.Fatalf("unexpected tool calls: %#v", response.ToolCalls)
	}
}

func TestLlamaCPPClientReturnsNativeToolCalls(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Berlin\"}"}}]}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "llamacpp",
		Endpoint: server.URL,
		Model:    "gemma4",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
		Tools: []ToolDefinition{
			{
				Name:        "lookup_weather",
				Description: "Look up weather for a city.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if len(response.ToolCalls) != 1 {
		t.Fatalf("expected 1 native tool call response, got %#v", response)
	}
	if response.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("unexpected tool name: %#v", response.ToolCalls)
	}
	if response.ToolCalls[0].Arguments["city"] != "Berlin" {
		t.Fatalf("unexpected tool arguments: %#v", response.ToolCalls[0].Arguments)
	}
	if _, ok := payload["tools"].([]any); !ok {
		t.Fatalf("expected tools in llama.cpp payload, got %#v", payload["tools"])
	}
}

func TestOpenAICompatibleClientReturnsNativeToolCalls(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Berlin\"}"}}]}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "openai-compatible",
		Endpoint: server.URL,
		Model:    "demo-model",
		APIKey:   "secret-token",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	response, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Prompt: "user prompt",
		Tools: []ToolDefinition{
			{
				Name:        "lookup_weather",
				Description: "Look up weather for a city.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if len(response.ToolCalls) != 1 {
		t.Fatalf("expected 1 native tool call response, got %#v", response)
	}
	if response.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("unexpected tool name: %#v", response.ToolCalls)
	}
	if response.ToolCalls[0].Arguments["city"] != "Berlin" {
		t.Fatalf("unexpected tool arguments: %#v", response.ToolCalls[0].Arguments)
	}
	if _, ok := payload["tools"].([]any); !ok {
		t.Fatalf("expected tools in openai-compatible payload, got %#v", payload["tools"])
	}
}

func TestGroqClientClampsZeroTemperature(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"return\",\"value\":\"ok\"}"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		Provider: "groq",
		Endpoint: server.URL,
		Model:    "openai/gpt-oss-20b",
		APIKey:   "secret-token",
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	zero := 0.0
	if _, err := client.Generate(context.Background(), Request{
		System:      "system prompt",
		Prompt:      "user prompt",
		Temperature: &zero,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if payload["temperature"] != float64(1e-8) {
		t.Fatalf("expected groq zero temperature to be clamped to 1e-8, got %#v", payload["temperature"])
	}
}
