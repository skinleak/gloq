# Changelog

All notable changes to gloq are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and gloq follows
[semantic versioning](https://semver.org/).

## [Unreleased]

### Added

- `WithPreset` with `PresetGCP` and `PresetDatadog`: JSON output uses the
  field names and severities Google Cloud Logging or Datadog recognize.
- `gloqtest` package: a `Recorder` handler that keeps every record, with
  `Find`, `Count`, `AssertLogged`, and `AssertNotLogged` for checking what
  code logged.
- `gloqotel.GCPContextAttrs`, which links records to their traces in Google
  Cloud Trace.

### Changed

- Pretty output writes attributes from `WithContextAttrs` after the record's
  own attributes, as JSON output already did.

## [1.2.0] - 2026-10-07

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
- JSON output is encoded by gloq itself instead of `slog.JSONHandler`, and
  is now faster than `slog.JSONHandler`. Records take less than half as long
  as before, and errors and source locations need no allocations. Output is
  unchanged apart from the fixes below.

### Fixed

- Newlines and terminal control sequences in logged data are escaped in pretty
  output, so logged input cannot forge log lines.
- JSON output stays valid when every attribute of a group is removed, such as
  by `WithReplaceAttr` or an empty `*slog.Source`; the group is left out.
  Later `WithReplaceAttr` calls no longer see the removed group.
- A time outside the years 0 to 9999 is written as a JSON string instead of
  producing invalid JSON.
- A panicking `MarshalJSON` method no longer crashes the program on Go 1.21.

## [1.1.0] - 2026-09-21

### Changed

- Faster logging paths and more reliable error rendering.

## [1.0.2] - 2026-09-21

- First release.

[Unreleased]: https://github.com/skinleak/gloq/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/skinleak/gloq/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/skinleak/gloq/compare/v1.0.2...v1.1.0
[1.0.2]: https://github.com/skinleak/gloq/releases/tag/v1.0.2
