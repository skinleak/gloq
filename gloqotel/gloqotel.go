// Package gloqotel adds OpenTelemetry trace and span IDs to log records, so
// logs can be correlated with traces.
//
// It is a separate module so that gloq itself has no dependencies. It works
// with any slog handler that accepts context attributes, such as gloq:
//
//	log := gloq.NewLogger(gloq.WithContextAttrs(gloqotel.ContextAttrs))
//	log.InfoContext(ctx, "request complete")
//	// ... trace_id=4bf92f3577b34da6a3ce929d0e0e4736 span_id=00f067aa0ba902b7
//
// Records are only correlated when they are logged with a context method,
// such as InfoContext, and that context holds a valid span. To export the
// logs themselves through OpenTelemetry, use the otelslog bridge instead.
package gloqotel

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Attribute keys, as used by the OpenTelemetry logs data model.
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// ContextAttrs returns the trace and span IDs of the span in ctx, or nil when
// ctx holds no valid span. Pass it to gloq.WithContextAttrs.
func ContextAttrs(ctx context.Context) []slog.Attr {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return nil
	}
	return []slog.Attr{
		slog.String(TraceIDKey, span.TraceID().String()),
		slog.String(SpanIDKey, span.SpanID().String()),
	}
}
