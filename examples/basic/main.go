package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/skinleak/gloq"
)

type requestIDKey struct{}

func main() {
	log := gloq.NewLogger(
		gloq.WithLevel(gloq.LevelTrace),
		gloq.WithLevelFromEnv("LOG_LEVEL"),
		gloq.WithContextAttrs(func(ctx context.Context) []slog.Attr {
			if id, ok := ctx.Value(requestIDKey{}).(string); ok {
				return []slog.Attr{slog.String("request_id", id)}
			}
			return nil
		}),
	)

	log.Debug("starting up...")
	log.Info("server is ready", "address", ":8080")
	log.Success("cache warmed", "entries", 128)
	log.Warn("connection is slow", "duration", "2s")

	ctx := context.WithValue(context.Background(), requestIDKey{}, "abc123")
	err := fmt.Errorf("query failed: %w", errors.New("connection refused"))
	log.With("service", "api").ErrorContext(ctx, "request failed", "status", 500, "error", err)

	loadUser(log)
}

func loadUser(log *gloq.Logger) {
	log.Trace("entering loadUser", "id", 42)
}
