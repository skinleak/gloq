package gloq

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// newReferenceJSONHandler builds gloq's original JSON handler: slog's own
// JSONHandler with gloq's features applied through ReplaceAttr. It is kept
// only as the reference the native encoder is compared against.
func newReferenceJSONHandler(out io.Writer, options ...Option) slog.Handler {
	c := defaultConfig()
	for _, option := range options {
		if option != nil {
			option(&c)
		}
	}
	pipeline := attrPipeline{transforms: c.attrTransforms, errorStack: c.errorStack}
	return &referenceJSONHandler{
		inner: slog.NewJSONHandler(out, &slog.HandlerOptions{
			AddSource: c.addSource,
			Level:     c.level,
			ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
				return referenceForJSON(pipeline, groups, attr)
			},
		}),
		location:     c.location,
		contextAttrs: c.contextAttrs,
	}
}

func referenceForJSON(p attrPipeline, groups []string, attr slog.Attr) slog.Attr {
	attr = p.apply(groups, attr)
	if attr.Equal(slog.Attr{}) {
		return slog.Attr{}
	}
	if attr.Value.Kind() != slog.KindAny {
		return attr
	}
	value := attr.Value.Any()
	if level, ok := value.(slog.Level); ok && attr.Key == slog.LevelKey {
		switch level {
		case LevelTrace, slog.LevelDebug, slog.LevelInfo, LevelSuccess, slog.LevelWarn, slog.LevelError, LevelFatal:
			return slog.String(slog.LevelKey, levelName(level))
		}
	}
	if stack, ok := value.(traceStack); ok {
		return slog.Any(attr.Key, []traceFrame(stack))
	}
	if err, ok := value.(error); ok {
		attr.Value = slog.AnyValue(describeError(err, p.errorStack, 0))
	}
	return attr
}

type referenceJSONHandler struct {
	inner        slog.Handler
	location     *time.Location
	contextAttrs []func(context.Context) []slog.Attr
}

func (h *referenceJSONHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *referenceJSONHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.location != nil && !record.Time.IsZero() {
		record.Time = record.Time.In(h.location)
	}
	trace := record.Level <= LevelTrace
	if !trace && (len(h.contextAttrs) == 0 || ctx == nil) {
		return h.inner.Handle(ctx, record)
	}
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

func (h *referenceJSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.inner = h.inner.WithAttrs(attrs)
	return &clone
}

func (h *referenceJSONHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.inner = h.inner.WithGroup(name)
	return &clone
}
