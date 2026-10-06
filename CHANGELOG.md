# Changelog

All notable changes to gloq are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and gloq follows
[semantic versioning](https://semver.org/).

## [Unreleased]

### Added

- `gloqhttp` package: HTTP middleware that logs every request with its method,
  path, status, size, and duration, assigns request IDs, and logs panics.
- `gloqotel` module: adds OpenTelemetry `trace_id` and `span_id` to records.
  It is a separate module, so gloq itself stays dependency-free.
- `Fanout` sends records to several handlers, such as the terminal and a file.
- `Sample` limits how often records with the same level and message are
  written.
- `Logger`, a `*slog.Logger` with `Trace`, `Success`, and `Fatal` methods,
  created with `NewLogger` or `Wrap`.
- Context variants of every package-level helper, and `gloq.With`.
- `OnFatal` hooks that run before a `Fatal` call exits.
- `FormatAuto`, which writes pretty output to terminals and JSON elsewhere.
- Options `WithLevelFromEnv`, `WithSourcePath`, `WithTimeZone`,
  `WithReplaceAttr`, and `WithContextAttrs`.
- `NewTestLogger`, which writes to a test's log.
- `Level` and `ParseLevel` for reading levels from flags and configuration.
- Colors in Windows consoles, and the `NO_COLOR` and `FORCE_COLOR`
  environment variables.
- Testable examples for the package documentation.
- Changelog.

### Changed

- Every `TRACE` record carries its call stack, in both formats.
- Pretty output aligns level names and dims timestamps, sources, and keys.
- Much faster handlers: JSON records at built-in levels need no allocations,
  and pretty output formats attributes added with `With` only once.

### Fixed

- Newlines and terminal control sequences in logged data are escaped in pretty
  output, so logged input cannot forge log lines.

## [1.1.0] - 2026-09-21

### Changed

- Faster logging paths and more reliable error rendering.

## [1.0.2] - 2026-09-21

- First release.

[Unreleased]: https://github.com/skinleak/gloq/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/skinleak/gloq/compare/v1.0.2...v1.1.0
[1.0.2]: https://github.com/skinleak/gloq/releases/tag/v1.0.2
