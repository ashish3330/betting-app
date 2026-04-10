package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// TracingMiddleware wraps a handler with OpenTelemetry HTTP tracing. Each
// incoming request becomes the root (or child, when an upstream propagates
// a traceparent header) span for the request. The span name defaults to the
// route path, with the HTTP method recorded as an attribute.
//
// The operation name passed to otelhttp is the serviceName, so spans are
// grouped under the owning service in the backend UI.
func TracingMiddleware(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, serviceName)
	}
}
