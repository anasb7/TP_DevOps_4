package main

import (
	"context"
	"net/http"
	"os"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Standard OpenTelemetry variable: tracing stays off until it is set (exercise 9).
const otlpEndpointEnv = "OTEL_EXPORTER_OTLP_ENDPOINT"

// initTracing exports spans with OTLP/HTTP to $OTEL_EXPORTER_OTLP_ENDPOINT.
// The service name comes from $OTEL_SERVICE_NAME. Returns a shutdown function
// that flushes the pending spans.
func initTracing(ctx context.Context, client *redis.Client) (func(context.Context) error, error) {
	if os.Getenv(otlpEndpointEnv) == "" {
		logger.Info("tracing disabled", "reason", otlpEndpointEnv+" is not set")
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	// Export errors (wrong endpoint...) in the JSON logs, not as plain text
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("tracing export failed", "component", "otel", "error", err)
	}))

	// One child span per Redis command
	if err := redisotel.InstrumentTracing(client); err != nil {
		return nil, err
	}
	logger.Info("tracing enabled", "endpoint", os.Getenv(otlpEndpointEnv))
	return provider.Shutdown, nil
}

// withTracing creates one span per HTTP request. Probes and scrapes are not traced:
// they would be most of the traces and say nothing about users.
func withTracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http",
		otelhttp.WithFilter(func(req *http.Request) bool {
			return req.URL.Path != "/healthz" && req.URL.Path != "/metrics"
		}),
	)
}
