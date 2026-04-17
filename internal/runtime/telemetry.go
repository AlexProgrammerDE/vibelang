package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type telemetryManager struct {
	mu       sync.Mutex
	writer   io.Writer
	provider *sdktrace.TracerProvider
	tracer   oteltrace.Tracer
	spans    map[string]oteltrace.Span
}

func newTelemetryManager(writer io.Writer) *telemetryManager {
	return &telemetryManager{
		writer: writer,
		spans:  make(map[string]oteltrace.Span),
	}
}

func (t *telemetryManager) ConfigureStdout(ctx context.Context, serviceName string, pretty bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.provider != nil {
		_ = t.provider.Shutdown(ctx)
		t.provider = nil
	}

	options := []stdouttrace.Option{stdouttrace.WithWriter(t.writer)}
	if pretty {
		options = append(options, stdouttrace.WithPrettyPrint())
	}

	exporter, err := stdouttrace.New(options...)
	if err != nil {
		return err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", attribute.String("service.name", serviceName)),
	)
	if err != nil {
		return err
	}

	t.provider = sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(res),
	)
	t.tracer = t.provider.Tracer("vibelang")
	t.spans = make(map[string]oteltrace.Span)
	return nil
}

func (t *telemetryManager) ensure(ctx context.Context) error {
	if t.provider != nil {
		return nil
	}
	return t.ConfigureStdout(ctx, "vibelang", true)
}

func (t *telemetryManager) StartSpan(ctx context.Context, handleID, name string, attrs map[string]any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.provider == nil {
		return fmt.Errorf("opentelemetry is not configured")
	}
	_, span := t.tracer.Start(ctx, name, oteltrace.WithAttributes(otelAttributes(attrs)...))
	t.spans[handleID] = span
	return nil
}

func (t *telemetryManager) AddEvent(handleID, name string, attrs map[string]any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	span, ok := t.spans[handleID]
	if !ok {
		return fmt.Errorf("unknown span handle %q", handleID)
	}
	span.AddEvent(name, oteltrace.WithAttributes(otelAttributes(attrs)...))
	return nil
}

func (t *telemetryManager) EndSpan(handleID string, attrs map[string]any) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	span, ok := t.spans[handleID]
	if !ok {
		return false, nil
	}
	delete(t.spans, handleID)
	if len(attrs) > 0 {
		span.SetAttributes(otelAttributes(attrs)...)
	}
	span.End()
	return true, nil
}

func (t *telemetryManager) Flush(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.provider == nil {
		return nil
	}
	return t.provider.ForceFlush(ctx)
}

func otelAttributes(values map[string]any) []attribute.KeyValue {
	if len(values) == 0 {
		return nil
	}
	result := make([]attribute.KeyValue, 0, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			result = append(result, attribute.String(key, typed))
		case bool:
			result = append(result, attribute.Bool(key, typed))
		case int:
			result = append(result, attribute.Int64(key, int64(typed)))
		case int64:
			result = append(result, attribute.Int64(key, typed))
		case float64:
			result = append(result, attribute.Float64(key, typed))
		default:
			result = append(result, attribute.String(key, stringify(value)))
		}
	}
	return result
}
