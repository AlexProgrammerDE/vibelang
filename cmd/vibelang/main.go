package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vibelang/internal/model"
	"vibelang/internal/parser"
	"vibelang/internal/runtime"
)

var version = "dev"

func main() {
	provider := flag.String("provider", envOr("VIBE_PROVIDER", "ollama"), "model provider: ollama, llamacpp, openai, groq, or openai-compatible")
	endpoint := flag.String("endpoint", envOr("VIBE_ENDPOINT", ""), "provider endpoint URL")
	modelName := flag.String("model", envOr("VIBE_MODEL", "gemma4"), "local model name")
	apiKey := flag.String("api-key", envOr("VIBE_API_KEY", ""), "API key for remote providers such as openai, groq, or openai-compatible gateways")
	temperature := flag.Float64("temperature", envFloat("VIBE_TEMPERATURE", 0.2), "model temperature")
	maxTokens := flag.Int("max-tokens", envInt("VIBE_MAX_TOKENS", 768), "maximum tokens to generate per AI step")
	maxSteps := flag.Int("max-steps", envInt("VIBE_MAX_STEPS", 8), "maximum helper-call steps per AI function")
	maxDepth := flag.Int("max-depth", envInt("VIBE_MAX_DEPTH", 8), "maximum nested AI function call depth")
	timeout := flag.Duration("timeout", envDuration("VIBE_TIMEOUT", 2*time.Minute), "HTTP timeout for model requests")
	checkOnly := flag.Bool("check", envBool("VIBE_CHECK", false), "parse the file and exit without running the model")
	trace := flag.Bool("trace", envBool("VIBE_TRACE", false), "write AI execution trace to stderr")
	showVersion := flag.Bool("version", false, "print the interpreter version and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] <file.vibe>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	sourcePath := flag.Arg(0)
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		fatalf("read %s: %v", sourcePath, err)
	}

	program, err := parser.ParseSource(string(source))
	if err != nil {
		fatalf("parse %s: %v", sourcePath, err)
	}
	if *checkOnly {
		return
	}

	resolvedAPIKey := strings.TrimSpace(*apiKey)
	if resolvedAPIKey == "" {
		switch *provider {
		case "openai":
			resolvedAPIKey = envOr("OPENAI_API_KEY", "")
		case "groq":
			resolvedAPIKey = envOr("GROQ_API_KEY", "")
		}
	}

	modelConfig := model.Config{
		Provider:       *provider,
		Endpoint:       *endpoint,
		Model:          *modelName,
		APIKey:         resolvedAPIKey,
		Temperature:    *temperature,
		HasTemperature: true,
		MaxTokens:      *maxTokens,
		Timeout:        *timeout,
	}

	client, err := model.NewClient(modelConfig)
	if err != nil {
		fatalf("configure model client: %v", err)
	}

	var traceWriter *os.File
	if *trace {
		traceWriter = os.Stderr
	}

	interpreter := runtime.NewInterpreter(runtime.Config{
		Model:        client,
		ModelConfig:  modelConfig,
		Stdout:       os.Stdout,
		Trace:        traceWriter,
		MaxAISteps:   *maxSteps,
		MaxCallDepth: *maxDepth,
	})

	if err := interpreter.ExecuteFile(context.Background(), program, sourcePath); err != nil {
		fatalf("run %s: %v", sourcePath, err)
	}
}

func envOr(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "No":
		return false
	default:
		return fallback
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
