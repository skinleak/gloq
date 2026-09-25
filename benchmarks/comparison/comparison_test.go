package comparison

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skinleak/gloq"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const message = "request complete"

var ctx = context.Background()

// Keep keys and values identical across libraries, including JSON durations in
// nanoseconds. Fixtures are prepared outside timed loops; construction is not
// being measured. The scaling suite deliberately uses these same prefixes.
func fixtures(n int) ([]slog.Attr, []zap.Field, []any) {
	attrs := make([]slog.Attr, n)
	fields := make([]zap.Field, n)
	args := make([]any, 0, 2*n)
	for i := 0; i < n; i++ {
		key := "field" + strconv.Itoa(i)
		var value any
		switch i % 5 {
		case 0:
			value = "abc123"
			attrs[i], fields[i] = slog.String(key, "abc123"), zap.String(key, "abc123")
		case 1:
			value = 200
			attrs[i], fields[i] = slog.Int(key, 200), zap.Int(key, 200)
		case 2:
			value = true
			attrs[i], fields[i] = slog.Bool(key, true), zap.Bool(key, true)
		case 3:
			value = 0.875
			attrs[i], fields[i] = slog.Float64(key, 0.875), zap.Float64(key, 0.875)
		case 4:
			value = 1250 * time.Microsecond
			attrs[i], fields[i] = slog.Duration(key, 1250*time.Microsecond), zap.Duration(key, 1250*time.Microsecond)
		}
		args = append(args, key, value)
	}
	return attrs, fields, args
}

func slogLogger(out io.Writer, implementation string, source bool) *slog.Logger {
	if implementation == "Gloq" {
		return slog.New(gloq.NewHandler(out, gloq.WithFormat(gloq.FormatJSON), gloq.WithSource(source)))
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: source}))
}

func zapLogger(out io.Writer, source bool) *zap.Logger {
	encoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey: "time", LevelKey: "level", MessageKey: "msg", CallerKey: "source",
		EncodeTime: zapcore.RFC3339NanoTimeEncoder, EncodeLevel: zapcore.CapitalLevelEncoder,
		EncodeDuration: zapcore.NanosDurationEncoder, EncodeCaller: zapcore.FullCallerEncoder,
		LineEnding: "\n",
	})
	// A locked sink matches the write serialization of the slog handlers.
	// No sampling, development mode, automatic stack traces or Fatal calls.
	core := zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(out)), zap.InfoLevel)
	return zap.New(core, zap.WithCaller(source))
}

func BenchmarkJSON(b *testing.B) {
	for _, n := range []int{0, 1, 3, 6, 10} {
		b.Run("Fields"+strconv.Itoa(n), func(b *testing.B) {
			for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
				b.Run(implementation, func(b *testing.B) {
					attrs, fields, _ := fixtures(n)
					b.ReportAllocs()
					if implementation == "Zap" {
						logger := zapLogger(io.Discard, false)
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							logger.Info(message, fields...)
						}
					} else {
						logger := slogLogger(io.Discard, implementation, false)
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
						}
					}
				})
			}
		})
	}
}

// Alternating arguments are compared separately from typed, prebuilt fields.
func BenchmarkAlternating(b *testing.B) {
	for _, implementation := range []string{"Gloq", "Slog", "ZapSugar"} {
		b.Run(implementation, func(b *testing.B) {
			_, _, args := fixtures(3)
			b.ReportAllocs()
			if implementation == "ZapSugar" {
				logger := zapLogger(io.Discard, false).Sugar()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					logger.Infow(message, args...)
				}
			} else {
				logger := slogLogger(io.Discard, implementation, false)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					logger.Info(message, args...)
				}
			}
		})
	}
}

var enabledResult bool

func BenchmarkDisabled(b *testing.B) {
	for _, operation := range []string{"Debug", "Enabled"} {
		b.Run(operation, func(b *testing.B) {
			for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
				b.Run(implementation, func(b *testing.B) {
					b.ReportAllocs()
					if implementation == "Zap" {
						logger := zapLogger(io.Discard, false)
						b.ResetTimer()
						if operation == "Enabled" {
							var enabled bool
							for i := 0; i < b.N; i++ {
								enabled = logger.Core().Enabled(zap.DebugLevel)
							}
							enabledResult = enabled
						} else {
							for i := 0; i < b.N; i++ {
								logger.Debug(message)
							}
						}
					} else {
						logger := slogLogger(io.Discard, implementation, false)
						b.ResetTimer()
						if operation == "Enabled" {
							var enabled bool
							for i := 0; i < b.N; i++ {
								enabled = logger.Enabled(ctx, slog.LevelDebug)
							}
							enabledResult = enabled
						} else {
							for i := 0; i < b.N; i++ {
								logger.Debug(message)
							}
						}
					}
				})
			}
		})
	}
	// Zap has no TRACE severity; do not invent a misleading mapping.
	for _, implementation := range []string{"Gloq", "Slog"} {
		b.Run("Trace/"+implementation, func(b *testing.B) {
			logger := slogLogger(io.Discard, implementation, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Log(ctx, gloq.LevelTrace, message)
			}
		})
	}
}

func BenchmarkContextFields(b *testing.B) {
	for _, scenario := range []string{"Bound3", "NestedGroups", "Source"} {
		b.Run(scenario, func(b *testing.B) {
			for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
				b.Run(implementation, func(b *testing.B) {
					attrs, fields, args := fixtures(3)
					b.ReportAllocs()
					if implementation == "Zap" {
						logger := zapLogger(io.Discard, scenario == "Source")
						if scenario == "Bound3" {
							logger = logger.With(fields...)
							fields = nil
						}
						if scenario == "NestedGroups" {
							logger = logger.With(zap.Namespace("http"), zap.Namespace("request"))
						}
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							logger.Info(message, fields...)
						}
					} else {
						logger := slogLogger(io.Discard, implementation, scenario == "Source")
						if scenario == "Bound3" {
							logger = logger.With(args...)
							attrs = nil
						}
						if scenario == "NestedGroups" {
							logger = logger.WithGroup("http").WithGroup("request")
						}
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
						}
					}
				})
			}
		})
	}
}

// These errors deliberately retain each library's native semantics. Gloq
// emits trees; slog and Zap emit strings for these stdlib error implementations.
// They measure the cost of those features, NOT equivalent serialization.
func BenchmarkNativeErrors(b *testing.B) {
	plain := errors.New("connection refused")
	for _, scenario := range []struct {
		name string
		err  error
	}{
		{"Plain", plain},
		{"Wrapped", fmt.Errorf("query failed: %w", plain)},
		{"Joined", errors.Join(plain, errors.New("cache unavailable"))},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
				b.Run(implementation, func(b *testing.B) {
					b.ReportAllocs()
					if implementation == "Zap" {
						logger := zapLogger(io.Discard, false)
						fields := []zap.Field{zap.Error(scenario.err)}
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							logger.Error(message, fields...)
						}
					} else {
						logger := slogLogger(io.Discard, implementation, false)
						attr := slog.Any("error", scenario.err)
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							logger.LogAttrs(ctx, slog.LevelError, message, attr)
						}
					}
				})
			}
		})
	}
}

func BenchmarkParallelJSON(b *testing.B) {
	for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
		b.Run(implementation, func(b *testing.B) {
			attrs, fields, _ := fixtures(3)
			b.ReportAllocs()
			if implementation == "Zap" {
				logger := zapLogger(io.Discard, false)
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						logger.Info(message, fields...)
					}
				})
			} else {
				logger := slogLogger(io.Discard, implementation, false)
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
					}
				})
			}
		})
	}
}

func BenchmarkScaling(b *testing.B) {
	for _, format := range []struct {
		name  string
		value gloq.Format
	}{
		{"Pretty", gloq.FormatPretty}, {"JSON", gloq.FormatJSON},
	} {
		b.Run(format.name, func(b *testing.B) {
			for _, api := range []string{"Typed", "Args"} {
				b.Run(api, func(b *testing.B) {
					for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 8, 10, 16, 32} {
						b.Run(strconv.Itoa(n), func(b *testing.B) {
							attrs, _, args := fixtures(n)
							logger := slog.New(gloq.NewHandler(io.Discard, gloq.WithFormat(format.value),
								gloq.WithSource(false), gloq.WithColor(gloq.ColorNever)))
							b.ReportAllocs()
							b.ResetTimer()
							if api == "Typed" {
								for i := 0; i < b.N; i++ {
									logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
								}
							} else {
								for i := 0; i < b.N; i++ {
									logger.Info(message, args...)
								}
							}
						})
					}
				})
			}
		})
	}
}

// Diagnostic controls: handler-only freezes the record's time and PC and
// excludes Enabled, clock, callers, record construction and attr insertion.
// The no-op ReplaceAttr isolates the stdlib callback machinery, not gloq.
func BenchmarkBoundary(b *testing.B) {
	attrs, _, _ := fixtures(3)
	for _, kind := range []string{"GloqJSON", "SlogJSON", "SlogJSONIdentity", "GloqPretty"} {
		b.Run(kind, func(b *testing.B) {
			var handler slog.Handler
			switch kind {
			case "GloqJSON":
				handler = gloq.NewHandler(io.Discard, gloq.WithFormat(gloq.FormatJSON), gloq.WithSource(false))
			case "GloqPretty":
				handler = gloq.NewHandler(io.Discard, gloq.WithSource(false), gloq.WithColor(gloq.ColorNever))
			default:
				opts := &slog.HandlerOptions{}
				if kind == "SlogJSONIdentity" {
					opts.ReplaceAttr = func(_ []string, attr slog.Attr) slog.Attr { return attr }
				}
				handler = slog.NewJSONHandler(io.Discard, opts)
			}
			b.Run("Logger", func(b *testing.B) {
				logger := slog.New(handler)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
				}
			})
			b.Run("Handle", func(b *testing.B) {
				var pcs [1]uintptr
				runtime.Callers(1, pcs[:])
				record := slog.NewRecord(time.Now(), slog.LevelInfo, message, pcs[0])
				record.AddAttrs(attrs...)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := handler.Handle(ctx, record); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// This diagnostic does real Record insertion and consumes each record,
// but omits output/encoding so it is never ranked against the real loggers.
type recordSink struct{ count int }

func (*recordSink) Enabled(context.Context, slog.Level) bool { return true }
func (s *recordSink) Handle(_ context.Context, r slog.Record) error {
	s.count += r.NumAttrs()
	return nil
}
func (s *recordSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *recordSink) WithGroup(string) slog.Handler      { return s }

var recordCount int

func BenchmarkRecord(b *testing.B) {
	for _, n := range []int{0, 5, 6, 10, 32} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			attrs, _, _ := fixtures(n)
			sink := &recordSink{}
			logger := slog.New(sink)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
			}
			recordCount = sink.count
		})
	}
}

// Verify comparison fixtures encode equal JSON values, rather than merely
// assuming similarly named constructors have equivalent wire semantics.
func TestJSONWorkloads(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
			t.Run(fmt.Sprintf("%s/groups=%v", implementation, grouped), func(t *testing.T) {
				attrs, fields, _ := fixtures(10)
				var output bytes.Buffer
				if implementation == "Zap" {
					logger := zapLogger(&output, false)
					if grouped {
						logger = logger.With(zap.Namespace("http"), zap.Namespace("request"))
					}
					logger.Info(message, fields...)
				} else {
					logger := slogLogger(&output, implementation, false)
					if grouped {
						logger = logger.WithGroup("http").WithGroup("request")
					}
					logger.LogAttrs(ctx, slog.LevelInfo, message, attrs...)
				}
				var record map[string]any
				if err := json.Unmarshal(output.Bytes(), &record); err != nil {
					t.Fatal(err)
				}
				if record["level"] != "INFO" || record["msg"] != message {
					t.Fatalf("builtins: %v", record)
				}
				if _, err := time.Parse(time.RFC3339Nano, record["time"].(string)); err != nil {
					t.Fatal(err)
				}
				values := record
				if grouped {
					values = record["http"].(map[string]any)["request"].(map[string]any)
				}
				for _, attr := range attrs {
					want := attr.Value.Any()
					switch attr.Value.Kind() {
					case slog.KindInt64:
						want = float64(attr.Value.Int64())
					case slog.KindDuration:
						want = float64(attr.Value.Duration())
					}
					if values[attr.Key] != want {
						t.Errorf("%s: got %v, want %v", attr.Key, values[attr.Key], want)
					}
				}
				if output.Bytes()[output.Len()-1] != '\n' {
					t.Fatal("missing newline")
				}
			})
		}
	}
}

func TestConfigurations(t *testing.T) {
	for _, implementation := range []string{"Gloq", "Slog", "Zap"} {
		t.Run(implementation, func(t *testing.T) {
			var output bytes.Buffer
			if implementation == "Zap" {
				logger := zapLogger(&output, true)
				logger.Debug("filtered")
				if output.Len() != 0 {
					t.Fatal("Debug must be disabled")
				}
				logger.Info(message)
			} else {
				logger := slogLogger(&output, implementation, true)
				logger.Debug("filtered")
				if output.Len() != 0 {
					t.Fatal("Debug must be disabled")
				}
				logger.Info(message)
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if implementation == "Zap" {
				source, ok := record["source"].(string)
				if !ok || !strings.Contains(source, "comparison_test.go:") {
					t.Fatalf("unexpected caller: %v", record["source"])
				}
			} else {
				source, ok := record["source"].(map[string]any)
				if !ok {
					t.Fatalf("unexpected source: %v", record["source"])
				}
				file, _ := source["file"].(string)
				function, _ := source["function"].(string)
				line, _ := source["line"].(float64)
				if !strings.HasSuffix(file, "comparison_test.go") ||
					!strings.Contains(function, "TestConfigurations") || line <= 0 {
					t.Fatalf("unexpected caller: %v", source)
				}
			}
		})
	}
}
