// Package gloq provides colorful structured logging for Go applications.
//
// Built on the standard library's [log/slog] package, gloq offers readable
// terminal output for local development and newline-delimited JSON for
// production log collectors. It supports structured attributes, child
// loggers, groups, source locations, automatic error-chain rendering, and
// TRACE, DEBUG, INFO, SUCCESS, WARN, ERROR, and FATAL log levels.
//
// For a ready-to-use logger, call [NewLogger]:
//
//	log := gloq.NewLogger()
//	log.Info("server started", "address", ":8080")
//	log.Success("cache warmed", "entries", 128)
//
// A [Logger] is a [*slog.Logger] with extra Trace, Success, and Fatal methods.
// Use [New] for a plain [*slog.Logger], [NewHandler] to attach gloq's handler
// to your own [slog.Logger], or the package-level logging functions for small
// applications. [NewTestLogger] sends output to a test's log.
//
// TRACE records include the call stack of the code that logged them. Logged
// messages, keys, values, and errors are escaped in pretty output, so
// untrusted data cannot forge log lines or send terminal control sequences.
package gloq
