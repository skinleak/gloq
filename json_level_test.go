package gloq

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"
)

var jsonLevelCases = []struct {
	name  string
	level slog.Level
	want  string
}{
	{"Trace", LevelTrace, "TRACE"},
	{"Debug", slog.LevelDebug, "DEBUG"},
	{"Info", slog.LevelInfo, "INFO"},
	{"Success", LevelSuccess, "SUCCESS"},
	{"Warn", slog.LevelWarn, "WARN"},
	{"Error", slog.LevelError, "ERROR"},
	{"Fatal", LevelFatal, "FATAL"},
	{"CustomNegative", slog.LevelDebug - 1, "DEBUG-1"},
	{"CustomBetween", slog.LevelInfo + 1, "INFO+1"},
	{"CustomAboveFatal", slog.LevelError + 5, "ERROR+5"},
	{"CustomHigh", slog.Level(100), "ERROR+92"},
}

func TestJSONLevelSerialization(t *testing.T) {
	for _, test := range jsonLevelCases {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(&output, WithFormat(FormatJSON), WithSource(false))
			record := slog.NewRecord(time.Time{}, test.level, "message", 0)
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			want := "{\"level\":\"" + test.want + "\",\"msg\":\"message\"}\n"
			if got := output.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestJSONLevelTransforms(t *testing.T) {
	for _, test := range []struct {
		name string
		attr slog.Attr
		want string
	}{
		{"typed", slog.Any(slog.LevelKey, slog.LevelWarn), `"level":"WARN",`},
		{"custom", slog.Any(slog.LevelKey, slog.Level(100)), `"level":"ERROR+92",`},
		{"resolved", slog.Any(slog.LevelKey, attrTestLogValuer(func() slog.Value {
			return slog.AnyValue(LevelSuccess)
		})), `"level":"SUCCESS",`},
		{"renamed", slog.Any("severity", LevelSuccess), `"severity":"INFO+2",`},
		{"string", slog.String(slog.LevelKey, "custom\"\n"), `"level":"custom\"\n",`},
		{"integer", slog.Int(slog.LevelKey, 2), `"level":2,`},
		{"removed", slog.Attr{}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			calls := 0
			handler := NewHandler(&output, WithFormat(FormatJSON), WithSource(false),
				withAttrTransform(func(groups []string, attr slog.Attr) slog.Attr {
					if attr.Key == slog.LevelKey {
						calls++
						if len(groups) != 0 || attr.Value.Kind() != slog.KindAny || attr.Value.Any() != slog.LevelInfo {
							t.Fatalf("transform must see original typed level: %v, groups %v", attr, groups)
						}
						return test.attr
					}
					return attr
				}))
			if err := handler.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0)); err != nil {
				t.Fatal(err)
			}
			want := "{" + test.want + "\"msg\":\"message\"}\n"
			if got := output.String(); got != want || calls != 1 {
				t.Fatalf("output = %q, want %q; transform calls = %d, want 1", got, want, calls)
			}
		})
	}
}

func TestJSONGroupedLevelAttrs(t *testing.T) {
	for _, test := range jsonLevelCases {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(&output, WithFormat(FormatJSON), WithSource(false)).
				WithGroup("outer").WithAttrs([]slog.Attr{slog.Any(slog.LevelKey, test.level)}).
				WithGroup("inner")
			record := slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0)
			record.AddAttrs(slog.Any(slog.LevelKey, test.level))
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			want := "{\"level\":\"INFO\",\"msg\":\"message\",\"outer\":{\"level\":\"" + test.want + "\",\"inner\":{\"level\":\"" + test.want + "\"}}}\n"
			if got := output.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestJSONDynamicLevels(t *testing.T) {
	var minimum slog.LevelVar
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithFormat(FormatJSON), WithSource(false), WithLevel(&minimum)))
	for _, threshold := range []slog.Level{slog.LevelWarn, LevelSuccess, LevelTrace, 100} {
		minimum.Set(threshold)
		for _, test := range jsonLevelCases {
			output.Reset()
			logger.Log(context.Background(), test.level, "message")
			if got, want := output.Len() > 0, test.level >= threshold; got != want {
				t.Fatalf("minimum %v, level %v: emitted = %v, want %v", threshold, test.level, got, want)
			}
		}
	}
}

// Raw TRACE and FATAL records have no stack-capture or process-exit side effects.
// Keep construction out of the loop and source disabled to isolate JSON levels.
func BenchmarkJSONLevels(b *testing.B) {
	for _, test := range jsonLevelCases {
		b.Run(test.name, func(b *testing.B) {
			logger := benchmarkLogger(WithFormat(FormatJSON), WithSource(false), WithLevel(LevelTrace))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Log(benchmarkContext, test.level, "message")
			}
		})
	}
}
