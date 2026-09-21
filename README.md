<p align="center">
  <img src="media/gloq.png" alt="gloq logo" width="160">
</p>

<h1 align="center">gloq</h1>

<p align="center">
  Colorful structured logging for Go, built on <code>log/slog</code>.
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/skinleak/gloq"><img src="https://pkg.go.dev/badge/github.com/skinleak/gloq.svg" alt="Go Reference"></a>
  <a href="https://github.com/skinleak/gloq/actions/workflows/ci.yml"><img src="https://github.com/skinleak/gloq/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/skinleak/gloq" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</p>

gloq is a lightweight structured logging library for Go. Inspired by tslog and
built directly on the standard [`log/slog`](https://pkg.go.dev/log/slog)
package, it combines readable, color-coded console logs for development with
structured JSON output for production.

## Features

- Color-coded, readable terminal output with automatic TTY detection
- Native `slog.Handler` compatibility, including attributes and groups
- Structured JSON output for log collectors and observability platforms
- `TRACE`, `DEBUG`, `INFO`, `SUCCESS`, `WARN`, `ERROR`, and `FATAL` log levels
- Source file and line information enabled by default
- Automatic rendering of wrapped and joined Go errors
- No third-party runtime dependencies

## Installation

```sh
go get github.com/skinleak/gloq
```

## Quick Start

```go
package main

import "github.com/skinleak/gloq"

func main() {
	gloq.Info("server is ready", "address", ":8080")
	gloq.Success("cache warmed", "entries", 128)
	gloq.Warn("connection is slow", "duration", "2s")
	gloq.Error("request failed", "status", 500)
}
```

Example output in a supported terminal:

```text
2026-09-21 10:30:15.123 INFO  [main.go:6] server is ready address=:8080
2026-09-21 10:30:15.124 SUCCESS [main.go:7] cache warmed entries=128
2026-09-21 10:30:15.125 WARN  [main.go:8] connection is slow duration=2s
2026-09-21 10:30:15.126 ERROR [main.go:9] request failed status=500
```

Because gloq implements Go's standard `slog.Handler` interface, child loggers,
structured attributes, groups, and context-aware methods work as expected:

```go
log := gloq.New()
requestLog := log.With("service", "api", "request_id", requestID)
requestLog.InfoContext(ctx, "request complete", "status", 200)
```

Colors are enabled automatically for terminals and disabled for redirected
output. Set `NO_COLOR=1` to disable them explicitly, or use
`gloq.WithColor(gloq.ColorNever)`.

## Log Levels

Built-in levels are ordered:

```text
TRACE < DEBUG < INFO < SUCCESS < WARN < ERROR < FATAL
```

| Level     | `slog` value | Description                                                         |
| --------- | -----------: | ------------------------------------------------------------------- |
| `TRACE`   |           -8 | Very detailed diagnostics with an automatic call stack              |
| `DEBUG`   |           -4 | Detailed diagnostic information                                     |
| `INFO`    |            0 | General informational messages                                      |
| `SUCCESS` |            2 | Successful or positive events                                       |
| `WARN`    |            4 | Potential problems that do not stop the program                     |
| `ERROR`   |            8 | Errors that prevent an operation from completing                    |
| `FATAL`   |           12 | Errors that stop the program; `gloq.Fatal` exits with status code 1 |

The default minimum level is `INFO`. Set a different threshold with
`gloq.WithLevel`:

```go
gloq.SetDefault(gloq.New(gloq.WithLevel(gloq.LevelTrace)))
gloq.Trace("entering request handler", "request_id", "abc123")
```

The package-level `Trace`, `Debug`, `Info`, `Success`, `Warn`, `Error`, and
`Fatal` helpers are available for small programs. `Trace` captures the current
call stack, while `Fatal` logs and exits with status code 1.

Logger instances remain standard `*slog.Logger` values. Use `Log` for gloq's
custom levels:

```go
logger.Log(ctx, gloq.LevelSuccess, "operation completed")
```

## JSON Logging

Switch to newline-delimited JSON for production logging:

```go
log := gloq.New(gloq.WithFormat(gloq.FormatJSON))
```

JSON output uses the standard slog field structure while preserving gloq's
custom level names and structured error handling.

## Structured Errors

Errors are rendered with their wrapped or joined causes automatically:

```go
gloq.Error("could not load user", "error", err)
```

JSON logs store the error message, concrete type, and causes as structured
fields. Use `gloq.WithErrorStack(true)` to include stack details provided by
the error value.

## Configuration

Pass options to `gloq.New` or `gloq.NewHandler`:

| Option           | Purpose                                                |
| ---------------- | ------------------------------------------------------ |
| `WithLevel`      | Set the minimum enabled log level                      |
| `WithFormat`     | Choose pretty terminal or JSON output                  |
| `WithColor`      | Enable, disable, or automatically detect color support |
| `WithSource`     | Show or hide the caller's source file and line         |
| `WithErrorStack` | Include stack information exposed by error values      |
| `WithTimeFormat` | Customize the timestamp layout                         |

See the [Go package documentation](https://pkg.go.dev/github.com/skinleak/gloq)
for the complete API.

## Compatibility

gloq supports Go 1.21 and newer and follows semantic versioning. Breaking API
changes will only be released in a new major version.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) to get
started. Report security issues privately as described in
[SECURITY.md](SECURITY.md).

## License

gloq is available under the [MIT License](LICENSE).
