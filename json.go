package gloq

import (
	"context"
	"log/slog"
	"time"
)

// jsonHandler adds gloq's per-record features to slog's JSON handler: TRACE
// stacks, context attributes, and time zone conversion.
type jsonHandler struct {
	inner        slog.Handler
	location     *time.Location
	contextAttrs []func(context.Context) []slog.Attr
}

func (h *jsonHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *jsonHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.location != nil && !record.Time.IsZero() {
		record.Time = record.Time.In(h.location)
	}
	trace := record.Level <= LevelTrace
	if !trace && (len(h.contextAttrs) == 0 || ctx == nil) {
		return h.inner.Handle(ctx, record)
	}

	// Records may share attribute storage with their copies, so clone before
	// adding to one.
	record = record.Clone()
	if ctx != nil {
		for _, extract := range h.contextAttrs {
			record.AddAttrs(extract(ctx)...)
		}
	}
	if trace {
		if stack := captureTraceStack(record.PC); len(stack) > 0 {
			record.AddAttrs(slog.Any(traceStackKey, stack))
		}
	}
	return h.inner.Handle(ctx, record)
}

func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.inner = h.inner.WithAttrs(attrs)
	return &clone
}

func (h *jsonHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.inner = h.inner.WithGroup(name)
	return &clone
}
