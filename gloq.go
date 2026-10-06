package gloq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Format describes how a log line is written.
type Format uint8

const (
	// FormatPretty is made for people reading a terminal.
	FormatPretty Format = iota
	// FormatJSON is made for tools and log collectors.
	FormatJSON
	// FormatAuto uses FormatPretty when writing to a terminal and FormatJSON
	// otherwise, such as when output is redirected to a file or collector.
	FormatAuto
)

// ColorMode decides when terminal colors are used.
type ColorMode uint8

const (
	// ColorAuto uses colors only when they make sense.
	ColorAuto ColorMode = iota
	// ColorAlways always uses colors.
	ColorAlways
	// ColorNever turns colors off.
	ColorNever
)

// SourcePath decides how much of a source file's path pretty output shows.
type SourcePath uint8

const (
	// SourceFile shows only the file name, such as "main.go:12".
	SourceFile SourcePath = iota
	// SourceDir adds the file's directory, such as "server/main.go:12".
	SourceDir
	// SourceFull shows the complete path.
	SourceFull
)

type config struct {
	level          slog.Leveler
	levelErr       error
	format         Format
	color          ColorMode
	addSource      bool
	sourcePath     SourcePath
	errorStack     bool
	timeFormat     string
	location       *time.Location
	attrTransforms []attrTransform
	contextAttrs   []func(context.Context) []slog.Attr
}

func defaultConfig() config {
	return config{
		level:      slog.LevelInfo,
		format:     FormatPretty,
		color:      ColorAuto,
		addSource:  true,
		timeFormat: "2006-01-02 15:04:05.000",
	}
}

// Option changes one logger setting.
type Option func(*config)

// WithLevel sets the lowest level that will be logged. Pass a
// [*slog.LevelVar] to change the level while the program is running.
func WithLevel(level slog.Leveler) Option {
	return func(c *config) {
		if level != nil {
			c.level, c.levelErr = level, nil
		}
	}
}

// WithLevelFromEnv reads the lowest level from an environment variable, such
// as LOG_LEVEL=debug, using [ParseLevel]. When the variable is unset the
// earlier level is kept. An invalid value is reported once with a WARN record
// when the handler is created, and the earlier level is kept.
func WithLevelFromEnv(key string) Option {
	return func(c *config) {
		value, ok := os.LookupEnv(key)
		if !ok || value == "" {
			return
		}
		level, err := ParseLevel(value)
		if err != nil {
			c.levelErr = fmt.Errorf("gloq: ignoring %s=%q: %w", key, value, err)
			return
		}
		c.level, c.levelErr = level, nil
	}
}

// WithFormat switches between pretty and JSON output.
func WithFormat(format Format) Option {
	return func(c *config) { c.format = format }
}

// WithColor controls terminal colors.
func WithColor(mode ColorMode) Option {
	return func(c *config) { c.color = mode }
}

// WithSource shows or hides the caller's file and line.
func WithSource(enabled bool) Option {
	return func(c *config) { c.addSource = enabled }
}

// WithSourcePath sets how much of the caller's file path pretty output shows.
// JSON output always contains the complete source.
func WithSourcePath(path SourcePath) Option {
	return func(c *config) { c.sourcePath = path }
}

// WithErrorStack includes stack details exposed by error values.
func WithErrorStack(enabled bool) Option {
	return func(c *config) { c.errorStack = enabled }
}

// WithTimeFormat sets the pretty timestamp layout.
func WithTimeFormat(layout string) Option {
	return func(c *config) {
		if layout != "" {
			c.timeFormat = layout
		}
	}
}

// WithTimeZone converts record timestamps to loc, such as [time.UTC], in both
// formats. By default timestamps keep the location they were created with.
func WithTimeZone(loc *time.Location) Option {
	return func(c *config) { c.location = loc }
}

// WithReplaceAttr rewrites or removes attributes before they are written, in
// the same way as [slog.HandlerOptions.ReplaceAttr]. Use it to redact secrets
// or rename keys. It is called for the built-in time, level, source, and
// message attributes too. Returning an empty [slog.Attr] removes the
// attribute. Several functions run in the order they were added.
func WithReplaceAttr(replace func(groups []string, attr slog.Attr) slog.Attr) Option {
	return func(c *config) {
		if replace != nil {
			c.attrTransforms = append(c.attrTransforms, replace)
		}
	}
}

// WithContextAttrs adds attributes taken from each record's context, such as
// trace, span, or request IDs. The function is called once for every enabled
// record and should be fast; it may return nil. The attributes are added like
// the record's own, inside any groups opened with WithGroup.
func WithContextAttrs(extract func(ctx context.Context) []slog.Attr) Option {
	return func(c *config) {
		if extract != nil {
			c.contextAttrs = append(c.contextAttrs, extract)
		}
	}
}

// New creates a logger that writes to standard error.
func New(options ...Option) *slog.Logger {
	return slog.New(NewHandler(os.Stderr, options...))
}

// NewHandler creates a handler for any slog logger.
func NewHandler(out io.Writer, options ...Option) slog.Handler {
	if out == nil {
		panic("gloq: nil writer")
	}

	c := defaultConfig()
	for _, option := range options {
		if option != nil {
			option(&c)
		}
	}
	pipeline := attrPipeline{
		transforms: c.attrTransforms,
		errorStack: c.errorStack,
	}
	format := c.format
	if format == FormatAuto {
		format = FormatJSON
		if isTerminal(out) {
			format = FormatPretty
		}
	}

	var handler slog.Handler
	if format == FormatJSON {
		handler = &jsonHandler{
			inner: slog.NewJSONHandler(out, &slog.HandlerOptions{
				AddSource: c.addSource,
				Level:     c.level,
				ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
					return pipeline.forJSON(groups, attr)
				},
			}),
			location:     c.location,
			contextAttrs: c.contextAttrs,
		}
	} else {
		handler = &prettyHandler{
			out: out,
			mu:  &sync.Mutex{},
			options: &prettyOptions{
				level:        c.level,
				addSource:    c.addSource,
				sourcePath:   c.sourcePath,
				errorStack:   c.errorStack,
				color:        shouldColor(out, c.color),
				timeFormat:   c.timeFormat,
				location:     c.location,
				pipeline:     pipeline,
				contextAttrs: c.contextAttrs,
			},
		}
	}

	if c.levelErr != nil {
		record := slog.NewRecord(time.Now(), slog.LevelWarn, c.levelErr.Error(), 0)
		_ = handler.Handle(context.Background(), record)
	}
	return handler
}
