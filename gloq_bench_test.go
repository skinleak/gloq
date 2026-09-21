package gloq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

var benchmarkContext = context.Background()

func benchmarkLogger(options ...Option) *slog.Logger {
	return slog.New(NewHandler(io.Discard, options...))
}

func useBenchmarkDefault(b *testing.B, logger *slog.Logger) {
	b.Helper()
	previous := Default()
	SetDefault(logger)
	b.Cleanup(func() { SetDefault(previous) })
}

func BenchmarkDisabledLogging(b *testing.B) {
	b.Run("PackageDebug", func(b *testing.B) {
		useBenchmarkDefault(b, benchmarkLogger(WithSource(false)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Debug("disabled")
		}
	})

	b.Run("PackageTrace", func(b *testing.B) {
		useBenchmarkDefault(b, benchmarkLogger(WithSource(false)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Trace("disabled")
		}
	})

	b.Run("LoggerDebug", func(b *testing.B) {
		logger := benchmarkLogger(WithSource(false))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Debug("disabled")
		}
	})

	b.Run("LoggerTrace", func(b *testing.B) {
		logger := benchmarkLogger(WithSource(false))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Log(benchmarkContext, LevelTrace, "disabled")
		}
	})
}

func BenchmarkPrettyBasic(b *testing.B) {
	tests := []struct {
		name  string
		level slog.Level
	}{
		{name: "Info", level: slog.LevelInfo},
		{name: "Success", level: LevelSuccess},
		{name: "Warn", level: slog.LevelWarn},
		{name: "Error", level: slog.LevelError},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			logger := benchmarkLogger(WithSource(false))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Log(benchmarkContext, test.level, "message")
			}
		})
	}
}

func BenchmarkPrettyAttributes(b *testing.B) {
	logger := benchmarkLogger(WithSource(false))

	b.Run("Alternating/1", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("message", "request_id", "abc123")
		}
	})

	b.Run("Alternating/3", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("message", "request_id", "abc123", "status", 200, "cached", true)
		}
	})

	b.Run("Alternating/6", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info(
				"message",
				"request_id", "abc123",
				"status", 200,
				"cached", true,
				"method", "GET",
				"path", "/users/42",
				"attempt", 2,
			)
		}
	})

	b.Run("LogAttrs/3", func(b *testing.B) {
		requestID := slog.String("request_id", "abc123")
		status := slog.Int("status", 200)
		cached := slog.Bool("cached", true)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.LogAttrs(benchmarkContext, slog.LevelInfo, "message", requestID, status, cached)
		}
	})
}

func BenchmarkPrettyBoundAttrs(b *testing.B) {
	logger := benchmarkLogger(WithSource(false)).With(
		"service", "api",
		"request_id", "abc123",
		"region", "eu-central-1",
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "status", 200)
	}
}

func BenchmarkPrettyGroups(b *testing.B) {
	b.Run("Single", func(b *testing.B) {
		logger := benchmarkLogger(WithSource(false)).WithGroup("request")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("message", "id", "abc123", "status", 200)
		}
	})

	b.Run("Nested", func(b *testing.B) {
		logger := benchmarkLogger(WithSource(false)).WithGroup("http").WithGroup("request")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("message", "id", "abc123", "status", 200)
		}
	})
}

func BenchmarkPrettySource(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "Disabled"
		if enabled {
			name = "Enabled"
		}
		b.Run(name, func(b *testing.B) {
			logger := benchmarkLogger(WithSource(enabled))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Info("message")
			}
		})
	}
}

func BenchmarkFormats(b *testing.B) {
	for _, format := range []struct {
		name   string
		option Option
	}{
		{name: "Pretty", option: WithFormat(FormatPretty)},
		{name: "JSON", option: WithFormat(FormatJSON)},
	} {
		b.Run(format.name, func(b *testing.B) {
			logger := benchmarkLogger(format.option, WithSource(false))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Info("message", "request_id", "abc123", "status", 200, "cached", true)
			}
		})
	}
}

func BenchmarkErrors(b *testing.B) {
	plain := errors.New("connection refused")
	wrapped := fmt.Errorf("query failed: %w", plain)
	joined := errors.Join(errors.New("cache unavailable"), errors.New("database unavailable"))

	tests := []struct {
		name string
		err  error
	}{
		{name: "Plain", err: plain},
		{name: "Wrapped", err: wrapped},
		{name: "Joined", err: joined},
	}
	formats := []struct {
		name   string
		option Option
	}{
		{name: "Pretty", option: WithFormat(FormatPretty)},
		{name: "JSON", option: WithFormat(FormatJSON)},
	}

	for _, format := range formats {
		b.Run(format.name, func(b *testing.B) {
			for _, test := range tests {
				b.Run(test.name, func(b *testing.B) {
					logger := benchmarkLogger(format.option, WithSource(false))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						logger.Error("message", "error", test.err)
					}
				})
			}
		})
	}

	b.Run("Pretty/Stack", func(b *testing.B) {
		logger := benchmarkLogger(WithSource(false), WithErrorStack(true))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Error("message", "error", stackError{})
		}
	})

	b.Run("JSON/Stack", func(b *testing.B) {
		logger := benchmarkLogger(WithFormat(FormatJSON), WithSource(false), WithErrorStack(true))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Error("message", "error", stackError{})
		}
	})
}

func BenchmarkTrace(b *testing.B) {
	b.Run("PackageHelper", func(b *testing.B) {
		useBenchmarkDefault(b, benchmarkLogger(WithLevel(LevelTrace), WithSource(false)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Trace("message")
		}
	})

	b.Run("RawLevel", func(b *testing.B) {
		logger := benchmarkLogger(WithLevel(LevelTrace), WithSource(false))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Log(benchmarkContext, LevelTrace, "message")
		}
	})
}

func BenchmarkFatalLevel(b *testing.B) {
	logger := benchmarkLogger(WithSource(false))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Log(benchmarkContext, LevelFatal, "message")
	}
}

func BenchmarkPackageVsLogger(b *testing.B) {
	b.Run("PackageInfo", func(b *testing.B) {
		useBenchmarkDefault(b, benchmarkLogger())
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Info("message")
		}
	})

	b.Run("LoggerInfo", func(b *testing.B) {
		logger := benchmarkLogger()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("message")
		}
	})
}

func BenchmarkParallel(b *testing.B) {
	for _, format := range []struct {
		name   string
		option Option
	}{
		{name: "Pretty", option: WithFormat(FormatPretty)},
		{name: "JSON", option: WithFormat(FormatJSON)},
	} {
		b.Run(format.name, func(b *testing.B) {
			logger := benchmarkLogger(format.option, WithSource(false))
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					logger.Info("message", "request_id", "abc123")
				}
			})
		})
	}
}
