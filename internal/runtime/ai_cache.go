package runtime

import (
	"context"
	"sort"
)

func registerAICacheBuiltins(interpreter *Interpreter) {
	registerBuiltin(interpreter, promptToolBuiltin("cache_stats", builtinCacheStats, "dict", "Return AI cache information, including current entry count and cache-related metrics."))
	registerBuiltin(interpreter, toolBuiltin("cache_clear", builtinCacheClear, "int", "Clear the AI result cache and return the number of removed entries."))
}

func builtinCacheStats(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("cache_stats", args, 0); err != nil {
		return nil, err
	}
	return interpreter.aiCacheStats(), nil
}

func builtinCacheClear(_ context.Context, interpreter *Interpreter, args []any) (any, error) {
	if err := expectArgCount("cache_clear", args, 0); err != nil {
		return nil, err
	}
	removed := interpreter.clearAICache()
	interpreter.incrementMetric("ai_cache_clears_total", 1)
	return int64(removed), nil
}

func (i *Interpreter) maybeLookupAICache(function *AIFunction, instructions string, args map[string]any, directives aiDirectiveConfig) (any, bool) {
	if directives.Cache == nil || !*directives.Cache {
		return nil, false
	}
	key := aiCacheKey(function, instructions, args, directives)
	value, ok := i.lookupAICache(key)
	if ok {
		i.incrementMetric("ai_cache_hits_total", 1)
		i.tracef("%s cache hit", function.Name())
		return value, true
	}
	i.incrementMetric("ai_cache_misses_total", 1)
	return key, false
}

func (i *Interpreter) maybeStoreAICache(cacheKey any, directives aiDirectiveConfig, value any) {
	if directives.Cache == nil || !*directives.Cache {
		return
	}
	key, ok := cacheKey.(string)
	if !ok || key == "" {
		return
	}
	i.storeAICache(key, value)
	i.incrementMetric("ai_cache_stores_total", 1)
}

func (i *Interpreter) lookupAICache(key string) (any, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	value, ok := i.aiCache[key]
	if !ok {
		return nil, false
	}
	return cloneValue(value), true
}

func (i *Interpreter) storeAICache(key string, value any) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.aiCache[key] = cloneValue(value)
}

func (i *Interpreter) clearAICache() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	removed := len(i.aiCache)
	i.aiCache = make(map[string]any)
	return removed
}

func (i *Interpreter) aiCacheStats() map[string]any {
	stats := i.metricsSnapshot()

	i.mu.RLock()
	stats["entries"] = int64(len(i.aiCache))
	i.mu.RUnlock()
	return stats
}

func aiCacheKey(function *AIFunction, instructions string, args map[string]any, directives aiDirectiveConfig) string {
	payload := map[string]any{
		"function":     function.Name(),
		"return_type":  function.Def.ReturnType.String(),
		"instructions": instructions,
		"args":         normalizeJSONValue(args),
		"directives": map[string]any{
			"temperature": directives.Temperature,
			"max_tokens":  directives.MaxTokens,
			"max_steps":   directives.MaxSteps,
			"timeout_ms":  directives.TimeoutMS,
			"cache":       directives.Cache,
			"tools":       sortedDirectiveNames(directives.AllowTools),
			"deny_tools":  sortedDirectiveNames(directives.DenyTools),
			"provider":    directives.Provider,
			"model":       directives.Model,
			"endpoint":    directives.Endpoint,
			"api_key_env": directives.APIKeyEnv,
		},
	}
	return jsonString(payload)
}

func sortedDirectiveNames(names map[string]struct{}) []any {
	if len(names) == 0 {
		return []any{}
	}
	items := make([]string, 0, len(names))
	for name := range names {
		items = append(items, name)
	}
	sort.Strings(items)
	result := make([]any, 0, len(items))
	for _, name := range items {
		result = append(result, name)
	}
	return result
}
