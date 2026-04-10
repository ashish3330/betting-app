package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the default instrumentation scope name used by StartSpan.
// Callers that want a different scope should use Tracer(name).Start(...).
const tracerName = "lotus"

// StartSpan starts a new span with the given name and optional attributes,
// returning the new context and the span handle. The caller is responsible
// for ending the span (typically via `defer span.End()`).
//
// Example:
//
//	ctx, span := telemetry.StartSpan(ctx, "wallet.debit",
//	    attribute.String("user_id", userID),
//	)
//	defer span.End()
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, name, trace.WithAttributes(attrs...))
}
