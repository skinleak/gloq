# gloq – Colorful Structured Logging for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/skinleak/gloq.svg)](https://pkg.go.dev/github.com/skinleak/gloq)
[![CI](https://github.com/skinleak/gloq/actions/workflows/ci.yml/badge.svg)](https://github.com/skinleak/gloq/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/skinleak/gloq)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

gloq is a lightweight, colorful structured logging library for Go. Built on
the standard [`log/slog`](https://pkg.go.dev/log/slog) package and inspired by
tslog, it combines human-friendly console logs for development with structured
JSON logging for production.

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

## Quick start

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
structured attributes, and groups work as expected:

```go
log := gloq.New()
requestLog := log.With("service", "api", "request_id", requestID)
requestLog.InfoContext(ctx, "request complete", "status", 200)
```

Colors are enabled automatically for terminals and disabled for redirected
output. Set `NO_COLOR=1` to disable them explicitly, or use
`gloq.WithColor(gloq.ColorNever)`.

## JSON logging

Switch to newline-delimited JSON for production logging:

```go
log := gloq.New(gloq.WithFormat(gloq.FormatJSON))
```

The package-level `Trace`, `Debug`, `Info`, `Success`, `Warn`, `Error`, and
`Fatal` functions are also available for small programs. `Trace` automatically
includes the current call stack, while `Fatal` exits with status code 1.

## Log levels

Built-in levels are ordered `TRACE < DEBUG < INFO < SUCCESS < WARN < ERROR < FATAL`.

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
`gloq.WithLevel`, for example:

```go
gloq.SetDefault(gloq.New(gloq.WithLevel(gloq.LevelTrace)))
gloq.Trace("entering request handler", "request_id", "abc123")
```

## Structured errors

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

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) to get started.
For security issues, see [SECURITY.md](SECURITY.md).

## License

This project is available under the [MIT License](LICENSE).
