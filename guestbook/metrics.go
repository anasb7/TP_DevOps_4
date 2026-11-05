package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/urfave/negroni"
	"go.opentelemetry.io/otel/trace"
)

// JSON logs on stdout: the collector (Grafana Alloy) ships them to Loki, `| json` parses them.
var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("version", os.Getenv("APP_VERSION"))

// redisLogger sends go-redis internal messages (connection errors...) to the JSON logger:
// its default text output would break the one-JSON-object-per-line format.
type redisLogger struct{}

func (redisLogger) Printf(ctx context.Context, format string, v ...interface{}) {
	logger.WarnContext(ctx, fmt.Sprintf(format, v...), "component", "redis")
}

// Label used for requests that match no API route (UI static files, 404s).
const staticHandler = "static"

var httpRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Number of HTTP requests, by route, method and status code.",
	},
	[]string{"handler", "method", "code"},
)

// TODO (exercise 3): http_request_duration_seconds

// A cause metric: alert on it to know *why* users get errors, not to page someone.
var redisUp = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "guestbook_redis_up",
	Help: "1 if the last Redis ping (from /healthz) succeeded, 0 otherwise.",
})

// routeName returns the route template (e.g. /lrange/{key}), never the real URL:
// one label value per message would explode the number of time series.
func routeName(router *mux.Router, req *http.Request) string {
	var match mux.RouteMatch
	if !router.Match(req, &match) || match.Route == nil {
		return staticHandler
	}
	template, err := match.Route.GetPathTemplate()
	if err != nil {
		return staticHandler
	}
	return template
}

// MetricsMiddleware records and logs every request, including the ones that panic
// (the handlers panic when Redis is down).
func MetricsMiddleware(router *mux.Router) negroni.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		handler := routeName(router, req)
		// Name the HTTP span after the route, like the metrics (no-op when tracing is off)
		trace.SpanFromContext(req.Context()).SetName(req.Method + " " + handler)
		res := rw.(negroni.ResponseWriter)
		start := time.Now()

		defer func() {
			code := res.Status()
			errMsg := ""
			// Handled here instead of negroni's Recovery: its multi-line stack trace
			// would break the one-JSON-object-per-line format expected by Loki.
			if p := recover(); p != nil {
				code = http.StatusInternalServerError
				errMsg = fmt.Sprint(p)
				if !res.Written() {
					res.WriteHeader(code)
				}
			}
			httpRequestsTotal.WithLabelValues(handler, req.Method, strconv.Itoa(code)).Inc()
			duration := time.Since(start)
			logger.Log(req.Context(), logLevel(code), "request",
				"method", req.Method,
				"handler", handler,
				"path", req.URL.Path,
				"code", code,
				"duration_ms", duration.Milliseconds(),
				"error", errMsg,
				"trace_id", traceID(req),
			)
		}()

		next(rw, req)
	}
}

// logLevel maps a status code to a level, so errors can be filtered with `level="ERROR"`.
func logLevel(code int) slog.Level {
	switch {
	case code >= http.StatusInternalServerError:
		return slog.LevelError
	case code >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// traceID links a log line to its trace in Tempo. Empty when tracing is off.
func traceID(req *http.Request) string {
	spanCtx := trace.SpanContextFromContext(req.Context())
	if !spanCtx.HasTraceID() {
		return ""
	}
	return spanCtx.TraceID().String()
}
