package runtime

import (
	"fmt"
	"os"
	"strings"
	"time"

	"vibelang/internal/model"
)

func (i *Interpreter) modelClientForDirectives(directives aiDirectiveConfig) (model.Client, error) {
	if !directives.customModelRoute() {
		if i.model == nil {
			return nil, fmt.Errorf("no model client configured")
		}
		return i.model, nil
	}
	if i.modelFactory == nil {
		return nil, fmt.Errorf("model routing is unavailable because no model factory is configured")
	}
	if strings.TrimSpace(i.modelConfig.Provider) == "" {
		return nil, fmt.Errorf("AI directives @provider, @model, @endpoint, @api_key_env, and @timeout_ms require interpreter model configuration")
	}

	config := i.modelConfig
	if directives.Provider != "" {
		config.Provider = directives.Provider
	}
	if directives.Model != "" {
		config.Model = directives.Model
	}
	if directives.Endpoint != "" {
		config.Endpoint = directives.Endpoint
	}
	if directives.APIKeyEnv != "" {
		config.APIKey = strings.TrimSpace(os.Getenv(directives.APIKeyEnv))
		if config.APIKey == "" {
			return nil, fmt.Errorf("AI directive @api_key_env %q is not set", directives.APIKeyEnv)
		}
	}
	if directives.TimeoutMS != nil {
		config.Timeout = time.Duration(*directives.TimeoutMS) * time.Millisecond
	}
	config = config.WithDefaults()

	key := modelClientCacheKey(config)

	i.mu.RLock()
	client, ok := i.modelClients[key]
	i.mu.RUnlock()
	if ok {
		return client, nil
	}

	client, err := i.modelFactory(config)
	if err != nil {
		return nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	if cached, ok := i.modelClients[key]; ok {
		return cached, nil
	}
	i.modelClients[key] = client
	return client, nil
}

func modelClientCacheKey(config model.Config) string {
	payload := map[string]any{
		"provider":        config.Provider,
		"endpoint":        config.Endpoint,
		"model":           config.Model,
		"api_key":         config.APIKey,
		"temperature":     config.Temperature,
		"has_temperature": config.HasTemperature,
		"max_tokens":      config.MaxTokens,
		"timeout_ms":      config.Timeout.Milliseconds(),
	}
	return jsonString(payload)
}
