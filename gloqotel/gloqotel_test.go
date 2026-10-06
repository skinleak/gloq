package gloqotel_test

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/skinleak/gloq/gloqotel"
)

func spanContext(t testing.TB) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatal(err)
	}
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	return trace.ContextWithSpanContext(context.Background(), span)
}

func TestContextAttrs(t *testing.T) {
	attrs := gloqotel.ContextAttrs(spanContext(t))
	want := []slog.Attr{
		slog.String("trace_id", "4bf92f3577b34da6a3ce929d0e0e4736"),
		slog.String("span_id", "00f067aa0ba902b7"),
	}
	if len(attrs) != len(want) {
		t.Fatalf("ContextAttrs() = %v, want %v", attrs, want)
	}
	for index := range want {
		if !attrs[index].Equal(want[index]) {
			t.Fatalf("ContextAttrs() = %v, want %v", attrs, want)
		}
	}
}

func TestContextAttrsWithoutSpan(t *testing.T) {
	if attrs := gloqotel.ContextAttrs(context.Background()); attrs != nil {
		t.Fatalf("ContextAttrs() = %v, want nil", attrs)
	}
}

func BenchmarkContextAttrs(b *testing.B) {
	ctx := spanContext(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_ = gloqotel.ContextAttrs(ctx)
	}
}
