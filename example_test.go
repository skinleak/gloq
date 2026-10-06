package gloq_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/skinleak/gloq"
)

// withoutTime removes timestamps so the examples have stable output.
func withoutTime(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey && len(groups) == 0 {
		return slog.Attr{}
	}
	return attr
}

// exampleLogger writes pretty output without color, time, or source to
// standard output.
func exampleLogger(options ...gloq.Option) *gloq.Logger {
	options = append([]gloq.Option{
		gloq.WithColor(gloq.ColorNever),
		gloq.WithSource(false),
		gloq.WithReplaceAttr(withoutTime),
	}, options...)
	return gloq.Wrap(slog.New(gloq.NewHandler(os.Stdout, options...)))
}

func Example() {
	log := exampleLogger()

	log.Info("server is ready", "address", ":8080")
	log.Success("cache warmed", "entries", 128)
	log.Warn("connection is slow", "duration", 2*time.Second)
	log.Error("request failed", "status", 500)
	// Output:
	// INFO    server is ready address=:8080
	// SUCCESS cache warmed entries=128
	// WARN    connection is slow duration=2s
	// ERROR   request failed status=500
}

func ExampleNewHandler() {
	handler := gloq.NewHandler(os.Stdout,
		gloq.WithColor(gloq.ColorNever),
		gloq.WithSource(false),
		gloq.WithReplaceAttr(withoutTime),
	)
	log := slog.New(handler)

	log.Info("plain slog logger", "user", "ada")
	// Output:
	// INFO    plain slog logger user=ada
}

func ExampleLogger_With() {
	log := exampleLogger()
	requestLog := log.With("request_id", "f3a9").WithGroup("http")

	requestLog.Info("request complete", "status", 200, "path", "/orders")
	// Output:
	// INFO    request complete request_id=f3a9 http.status=200 http.path=/orders
}

func Example_errors() {
	log := exampleLogger()
	err := fmt.Errorf("load config: %w", &fs.PathError{Op: "open", Path: "app.yaml", Err: fs.ErrNotExist})

	log.Error("startup failed", "error", err)
	// Output:
	// ERROR   startup failed
	//   error: load config
	//     caused by: open app.yaml
	//       caused by: file does not exist
}

func ExampleWithFormat() {
	log := slog.New(gloq.NewHandler(os.Stdout,
		gloq.WithFormat(gloq.FormatJSON),
		gloq.WithSource(false),
		gloq.WithReplaceAttr(withoutTime),
	))

	log.Info("order placed", "order_id", 42, "error", errors.New("card declined"))
	// Output:
	// {"level":"INFO","msg":"order placed","order_id":42,"error":{"message":"card declined","type":"*errors.errorString"}}
}

func ExampleWithReplaceAttr() {
	log := exampleLogger(gloq.WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == "password" {
			return slog.String("password", "[REDACTED]")
		}
		return attr
	}))

	log.Info("user signed in", "user", "ada", "password", "hunter2")
	// Output:
	// INFO    user signed in user=ada password=[REDACTED]
}

func ExampleWithContextAttrs() {
	type tenantKey struct{}
	log := exampleLogger(gloq.WithContextAttrs(func(ctx context.Context) []slog.Attr {
		if tenant, ok := ctx.Value(tenantKey{}).(string); ok {
			return []slog.Attr{slog.String("tenant", tenant)}
		}
		return nil
	}))

	ctx := context.WithValue(context.Background(), tenantKey{}, "acme")
	log.InfoContext(ctx, "invoice sent")
	// Output:
	// INFO    invoice sent tenant=acme
}

func ExampleWithLevel() {
	var level slog.LevelVar
	log := exampleLogger(gloq.WithLevel(&level))

	log.Debug("hidden")
	level.Set(slog.LevelDebug)
	log.Debug("visible after changing the level")
	// Output:
	// DEBUG   visible after changing the level
}

func ExampleParseLevel() {
	for _, text := range []string{"trace", "Success", "INFO+2", "fatal"} {
		level, err := gloq.ParseLevel(text)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println(text, "=", int(level))
	}
	// Output:
	// trace = -8
	// Success = 2
	// INFO+2 = 2
	// fatal = 12
}

func ExampleLevel() {
	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	level := gloq.Level(slog.LevelInfo)
	flags.TextVar(&level, "log-level", level, "minimum log level")

	_ = flags.Parse([]string{"-log-level", "success"})
	fmt.Println(level)
	// Output:
	// SUCCESS
}

func ExampleFanout() {
	handler := gloq.Fanout(
		gloq.NewHandler(os.Stdout,
			gloq.WithColor(gloq.ColorNever),
			gloq.WithSource(false),
			gloq.WithReplaceAttr(withoutTime),
		),
		gloq.NewHandler(os.Stdout,
			gloq.WithFormat(gloq.FormatJSON),
			gloq.WithLevel(slog.LevelWarn),
			gloq.WithSource(false),
			gloq.WithReplaceAttr(withoutTime),
		),
	)
	log := slog.New(handler)

	log.Info("only pretty")
	log.Warn("pretty and JSON")
	// Output:
	// INFO    only pretty
	// WARN    pretty and JSON
	// {"level":"WARN","msg":"pretty and JSON"}
}

func ExampleSample() {
	handler := gloq.Sample(
		gloq.NewHandler(os.Stdout,
			gloq.WithColor(gloq.ColorNever),
			gloq.WithSource(false),
			gloq.WithReplaceAttr(withoutTime),
		),
		gloq.Sampling{Tick: time.Minute, First: 2, Thereafter: 3},
	)
	log := slog.New(handler)

	for attempt := 1; attempt <= 8; attempt++ {
		log.Warn("retrying", "attempt", attempt)
	}
	// Output:
	// WARN    retrying attempt=1
	// WARN    retrying attempt=2
	// WARN    retrying attempt=5
	// WARN    retrying attempt=8
}

func ExampleOnFatal() {
	gloq.OnFatal(func() {
		// Flush buffers or shut down exporters here. Hooks run after the
		// FATAL record is written and before the program exits.
	})
}
