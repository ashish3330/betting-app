package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// SetupTracer configures the global tracer with an OTLP exporter.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is empty the function installs a no-op
// shutdown function and leaves the default global (noop) tracer provider
// untouched. This keeps tracing opt-in so unit tests and local dev don't
// require a running collector.
//
// The returned shutdown function MUST be called on graceful exit to flush
// any buffered spans to the collector.
func SetupTracer(ctx context.Context, serviceName, version string, log *slog.Logger) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		log.Info("tracing disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)", "service", serviceName)
		return func(context.Context) error { return nil }, nil
	}

	// otlptracehttp expects the endpoint as host:port without a scheme; strip
	// any scheme a user might have supplied in the env var.
	cleanEndpoint := endpoint
	if i := strings.Index(cleanEndpoint, "://"); i >= 0 {
		cleanEndpoint = cleanEndpoint[i+3:]
	}
	cleanEndpoint = strings.TrimSuffix(cleanEndpoint, "/")

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cleanEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Info("OTLP tracing enabled", "endpoint", cleanEndpoint, "service", serviceName)

	return tp.Shutdown, nil
}

// Tracer returns the global tracer for a given component name. Callers should
// prefer passing a stable instrumentation name (e.g. the package path) so that
// spans can be grouped by instrumentation scope in the backend UI.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
