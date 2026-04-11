package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// setupSampler returns a sampler configured from the OTEL_TRACES_SAMPLER_ARG
// environment variable, falling back to 1% sampling in production and full
// sampling otherwise. This avoids paying the full cost of span export on
// every request in high-traffic environments.
func setupSampler() sdktrace.Sampler {
	ratio := 1.0
	if r := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); r != "" {
		if v, err := strconv.ParseFloat(r, 64); err == nil {
			ratio = v
		}
	} else if os.Getenv("ENVIRONMENT") == "production" {
		ratio = 0.01 // 1% in production by default
	}
	if ratio >= 1.0 {
		return sdktrace.AlwaysSample()
	}
	if ratio <= 0.0 {
		return sdktrace.NeverSample()
	}
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
}

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
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(500*time.Millisecond),
			sdktrace.WithMaxQueueSize(4096),
			sdktrace.WithMaxExportBatchSize(1024),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(setupSampler()),
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
