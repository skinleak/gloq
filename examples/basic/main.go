package main

import (
	"errors"
	"fmt"

	"github.com/skinleak/gloq"
)

func main() {
	gloq.Debug("starting up...")
	gloq.Info("server is ready", "address", ":8080")
	gloq.Warn("connection is slow", "duration", "2s")

	err := fmt.Errorf("query failed: %w", errors.New("connection refused"))
	gloq.Error("request failed", "status", 500, "error", err)
}
