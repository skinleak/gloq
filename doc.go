// Package gloq provides colorful structured logging for Go applications.
//
// Built on the standard library's [log/slog] package, gloq offers readable
// terminal output for local development and newline-delimited JSON for
// production log collectors. It supports structured attributes, child
// loggers, groups, source locations, automatic error-chain rendering, and
// TRACE through FATAL log levels.
//
// For a ready-to-use logger, call [New]:
//
//	log := gloq.New()
//	log.Info("server started", "address", ":8080")
//
// Use [NewHandler] to attach gloq's handler to an existing [slog.Logger], or
// use the package-level logging functions for small applications.
package gloq
