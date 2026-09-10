package observability

import (
	"context"
	"log/slog"
)

// NewLogHandler enriches context-aware records with the middleware's request_id.
// Use slog's *Context methods; requestless records remain unchanged. The wrapped
// handler owns filtering, output, and concurrency safety. Attributes retain normal
// slog grouping: request_id is added in the active group. Reserve that key for this
// handler to avoid duplicates. No other context fields or request data are logged.
// This is not an error redactor or access logger. A nil handler panics.
func NewLogHandler(next slog.Handler) slog.Handler {
	if next == nil {
		panic("observability: nil log handler")
	}
	return &logHandler{next: next}
}

type logHandler struct {
	next slog.Handler
}

// Enabled leaves log-level filtering with the caller's handler.
func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle clones the record before adding correlation so sibling handlers are safe.
func (h *logHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestID(ctx); id != "" {
		record = record.Clone()
		record.AddAttrs(slog.String("request_id", id))
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs preserves the wrapped handler's attribute semantics.
func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup preserves the wrapped handler's group semantics.
func (h *logHandler) WithGroup(name string) slog.Handler {
	return &logHandler{next: h.next.WithGroup(name)}
}
