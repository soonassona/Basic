// Package observability provides a structured JSON logger that emits the
// fields section 13 of the spec requires (timestamp, level, service,
// trace_id, request_id, user_id, org_id, method, path, status, duration_ms,
// message). It wraps log/slog with a json handler.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey string

const (
	keyRequestID ctxKey = "request_id"
	keyUserID    ctxKey = "user_id"
	keyOrgID     ctxKey = "org_id"
)

// NewLogger returns a slog.Logger writing JSON lines to stdout.
func NewLogger(level string, service string) *slog.Logger {
	return newLoggerTo(os.Stdout, level, service)
}

func newLoggerTo(w io.Writer, level, service string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			} else if a.Key == slog.MessageKey {
				a.Key = "message"
			} else if a.Key == slog.LevelKey {
				a.Key = "level"
			}
			return a
		},
	})
	return slog.New(h).With("service", service)
}

// FromContext returns a logger enriched with request, trace, user, and org
// fields drawn from the context. Handlers should call this once per request.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	l := base
	if v, ok := ctx.Value(keyRequestID).(string); ok && v != "" {
		l = l.With("request_id", v)
	}
	if v, ok := ctx.Value(keyUserID).(string); ok && v != "" {
		l = l.With("user_id", v)
	}
	if v, ok := ctx.Value(keyOrgID).(string); ok && v != "" {
		l = l.With("org_id", v)
	}
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		l = l.With("trace_id", span.TraceID().String())
	}
	return l
}

// WithRequestID returns a derived context tagged with the given request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// WithUserID returns a derived context tagged with the given user id.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyUserID, id)
}

// WithOrgID returns a derived context tagged with the given organisation id.
func WithOrgID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyOrgID, id)
}
