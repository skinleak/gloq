package gloq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	traceColor   = "\x1b[1;97m"       // white bold
	debugColor   = "\x1b[1;96m"       // cyan bold
	infoColor    = "\x1b[1;94m"       // blue bold
	successColor = "\x1b[1;92m"       // green bold
	warnColor    = "\x1b[1;93m"       // yellow bold
	errorColor   = "\x1b[1;91m"       // red bold
	fatalColor   = "\x1b[1;38;5;208m" // orange bold

	resetColor = "\x1b[0m"
)

const (
	// LevelTrace is for very detailed diagnostics and execution tracing.
	LevelTrace = slog.LevelDebug - 4
	// LevelSuccess is for successful or positive events.
	LevelSuccess = slog.LevelInfo + 2
	// LevelFatal is for errors that stop the program.
	LevelFatal = slog.LevelError + 4
)

const traceStackKey = "stack"

type traceStack string

// Format describes how a log line is written.
type Format uint8

const (
	// FormatPretty is made for people reading a terminal.
	FormatPretty Format = iota
	// FormatJSON is made for tools and log collectors.
	FormatJSON
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

type config struct {
	level          slog.Leveler
	format         Format
	color          ColorMode
	addSource      bool
	errorStack     bool
	timeFormat     string
	attrTransforms []attrTransform
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

// WithLevel sets the lowest level that will be logged.
func WithLevel(level slog.Leveler) Option {
	return func(c *config) {
		if level != nil {
			c.level = level
		}
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

// WithErrorStack includes stack details exposed by error values.
func WithErrorStack(enabled bool) Option {
	return func(c *config) { c.errorStack = enabled }
}

// WithTimeFormat sets the timestamp layout.
func WithTimeFormat(layout string) Option {
	return func(c *config) {
		if layout != "" {
			c.timeFormat = layout
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

	if c.format == FormatJSON {
		return slog.NewJSONHandler(out, &slog.HandlerOptions{
			AddSource: c.addSource,
			Level:     c.level,
			ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
				return pipeline.forJSON(groups, attr)
			},
		})
	}

	return &prettyHandler{
		out:        out,
		mu:         &sync.Mutex{},
		level:      c.level,
		addSource:  c.addSource,
		errorStack: c.errorStack,
		color:      shouldColor(out, c.color),
		time:       c.timeFormat,
		pipeline:   pipeline,
	}
}

type boundAttr struct {
	groups []string
	attr   slog.Attr
}

type prettyHandler struct {
	out        io.Writer
	mu         *sync.Mutex
	level      slog.Leveler
	addSource  bool
	errorStack bool
	color      bool
	time       string
	groups     []string
	attrs      []boundAttr
	pipeline   attrPipeline
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *prettyHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	line.Grow(64)
	var errors []prettyError
	var stacks []traceStack
	if !record.Time.IsZero() {
		attr := h.pipeline.apply(nil, slog.Time(slog.TimeKey, record.Time))
		if !attr.Equal(slog.Attr{}) {
			if attr.Value.Kind() == slog.KindTime {
				var timestamp [64]byte
				_, _ = line.Write(attr.Value.Time().AppendFormat(timestamp[:0], h.time))
			} else {
				appendValue(&line, attr.Value)
			}
			line.WriteByte(' ')
		}
	}
	levelAttr := h.pipeline.apply(nil, slog.Any(slog.LevelKey, record.Level))
	if !levelAttr.Equal(slog.Attr{}) {
		if level, ok := levelAttr.Value.Any().(slog.Level); ok {
			h.writeLevel(&line, level)
		} else {
			appendValue(&line, levelAttr.Value)
		}
		line.WriteByte(' ')
	}

	if h.addSource && record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		if len(h.pipeline.transforms) == 0 {
			line.WriteByte('[')
			line.WriteString(filepath.Base(frame.File))
			line.WriteByte(':')
			line.WriteString(strconv.Itoa(frame.Line))
			line.WriteString("] ")
		} else {
			source := &slog.Source{File: frame.File, Line: frame.Line, Function: frame.Function}
			attr := h.pipeline.apply(nil, slog.Any(slog.SourceKey, source))
			if !attr.Equal(slog.Attr{}) {
				line.WriteByte('[')
				if value, ok := attr.Value.Any().(*slog.Source); ok {
					line.WriteString(filepath.Base(value.File))
					line.WriteByte(':')
					line.WriteString(strconv.Itoa(value.Line))
				} else {
					appendValue(&line, attr.Value)
				}
				line.WriteString("] ")
			}
		}
	}

	message := h.pipeline.apply(nil, slog.String(slog.MessageKey, record.Message))
	if !message.Equal(slog.Attr{}) {
		if message.Value.Kind() == slog.KindString {
			line.WriteString(message.Value.String())
		} else {
			appendValue(&line, message.Value)
		}
	}
	for _, item := range h.attrs {
		appendAttr(&line, item.groups, item.attr, &errors, &stacks, h.pipeline)
	}
	record.Attrs(func(attr slog.Attr) bool {
		appendAttr(&line, h.groups, attr, &errors, &stacks, h.pipeline)
		return true
	})
	for _, detail := range errors {
		appendPrettyError(&line, detail, h.errorStack)
	}
	if len(errors) == 0 {
		line.WriteByte('\n')
	}
	for _, stack := range stacks {
		appendPrettyTraceStack(&line, stack)
	}

	// Keep concurrent log lines from being mixed together.
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, line.String())
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := h.clone()
	for _, attr := range attrs {
		// Remember which groups were active when this field was added.
		clone.attrs = append(clone.attrs, boundAttr{
			groups: append([]string(nil), h.groups...),
			attr:   attr,
		})
	}
	return clone
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := h.clone()
	clone.groups = append(clone.groups, name)
	return clone
}

func (h *prettyHandler) clone() *prettyHandler {
	clone := *h
	clone.groups = append([]string(nil), h.groups...)
	clone.attrs = append([]boundAttr(nil), h.attrs...)
	return &clone
}

func (h *prettyHandler) writeLevel(line *strings.Builder, level slog.Level) {
	name, color := levelNameAndColor(level)
	if h.color {
		line.WriteString(color)
	}
	line.WriteString(name)
	if h.color {
		line.WriteString(resetColor)
	}
	if padding := 5 - len(name); padding > 0 {
		line.WriteString("     "[:padding])
	}
}

func levelNameAndColor(level slog.Level) (string, string) {
	switch {
	case level == LevelFatal:
		return "FATAL", fatalColor
	case level >= slog.LevelError:
		return level.String(), errorColor
	case level >= slog.LevelWarn:
		return level.String(), warnColor
	case level == LevelSuccess:
		return "SUCCESS", successColor
	case level >= slog.LevelInfo:
		return level.String(), infoColor
	case level >= slog.LevelDebug:
		return level.String(), debugColor
	case level == LevelTrace:
		return "TRACE", traceColor
	default:
		return level.String(), traceColor
	}
}

func appendAttr(
	line *strings.Builder,
	groups []string,
	attr slog.Attr,
	errors *[]prettyError,
	stacks *[]traceStack,
	pipeline attrPipeline,
) {
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			groups = append(append([]string(nil), groups...), attr.Key)
		}
		for _, child := range value.Group() {
			appendAttr(line, groups, child, errors, stacks, pipeline)
		}
		return
	}
	attr.Value = value
	attr = pipeline.applyResolved(groups, attr)
	if attr.Equal(slog.Attr{}) {
		return
	}
	value = attr.Value
	// Inspect the resolved, transformed value without boxing primitive kinds.
	if value.Kind() == slog.KindAny {
		special := value.Any()
		if stack, ok := special.(traceStack); ok {
			*stacks = append(*stacks, stack)
			return
		}
		if err, ok := special.(error); ok {
			*errors = append(*errors, prettyError{key: joinedKey(groups, attr.Key), err: err})
			return
		}
	}

	line.WriteByte(' ')
	appendJoinedKey(line, groups, attr.Key)
	line.WriteByte('=')
	appendValue(line, value)
}

func appendPrettyTraceStack(line *strings.Builder, stack traceStack) {
	line.WriteString("  stack:\n")
	remaining := string(stack)
	for {
		stackLine, rest, found := strings.Cut(remaining, "\n")
		line.WriteString("    ")
		line.WriteString(stackLine)
		line.WriteByte('\n')
		if !found {
			break
		}
		remaining = rest
	}
}

func joinedKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	if key == "" {
		return strings.Join(groups, ".")
	}
	return strings.Join(groups, ".") + "." + key
}

func appendJoinedKey(line *strings.Builder, groups []string, key string) {
	for index, group := range groups {
		if index > 0 {
			line.WriteByte('.')
		}
		line.WriteString(group)
	}
	if len(groups) > 0 && key != "" {
		line.WriteByte('.')
	}
	line.WriteString(key)
}

func appendValue(line *strings.Builder, value slog.Value) {
	switch value.Kind() {
	case slog.KindString:
		appendString(line, value.String())
	case slog.KindTime:
		var formatted [64]byte
		_, _ = line.Write(value.Time().AppendFormat(formatted[:0], time.RFC3339Nano))
	case slog.KindDuration:
		line.WriteString(value.Duration().String())
	case slog.KindInt64:
		var formatted [32]byte
		_, _ = line.Write(strconv.AppendInt(formatted[:0], value.Int64(), 10))
	case slog.KindUint64:
		var formatted [32]byte
		_, _ = line.Write(strconv.AppendUint(formatted[:0], value.Uint64(), 10))
	case slog.KindFloat64:
		var formatted [32]byte
		_, _ = line.Write(strconv.AppendFloat(formatted[:0], value.Float64(), 'g', -1, 64))
	case slog.KindBool:
		if value.Bool() {
			line.WriteString("true")
		} else {
			line.WriteString("false")
		}
	case slog.KindAny:
		if value.Any() == nil {
			line.WriteString("<nil>")
			return
		}
		if err, ok := value.Any().(error); ok {
			appendString(line, err.Error())
			return
		}
		appendString(line, fmt.Sprint(value.Any()))
	default:
		line.WriteString(value.String())
	}
}

func appendString(line *strings.Builder, value string) {
	if value == "" || strings.ContainsAny(value, " \t\r\n=\"") {
		var quoted [64]byte
		_, _ = line.Write(strconv.AppendQuote(quoted[:0], value))
		return
	}
	line.WriteString(value)
}

func shouldColor(out io.Writer, mode ColorMode) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}

	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

var defaultLogger atomic.Pointer[slog.Logger]

func init() {
	defaultLogger.Store(New())
}

// Default returns the logger used by the package helpers.
func Default() *slog.Logger {
	return defaultLogger.Load()
}

// SetDefault replaces the logger used by gloq and slog.
func SetDefault(logger *slog.Logger) {
	if logger == nil {
		panic("gloq: nil logger")
	}
	defaultLogger.Store(logger)
	slog.SetDefault(logger)
}

// Debug writes a debug message.
func Debug(message string, args ...any) {
	log(context.Background(), slog.LevelDebug, message, args...)
}

// Info writes an informational message.
func Info(message string, args ...any) {
	log(context.Background(), slog.LevelInfo, message, args...)
}

// Success writes a successful or positive event.
func Success(message string, args ...any) {
	log(context.Background(), LevelSuccess, message, args...)
}

// Trace writes a detailed diagnostic message with the current call stack.
func Trace(message string, args ...any) {
	log(context.Background(), LevelTrace, message, args...)
}

// Warn writes a warning message.
func Warn(message string, args ...any) {
	log(context.Background(), slog.LevelWarn, message, args...)
}

// Error writes an error message.
func Error(message string, args ...any) {
	log(context.Background(), slog.LevelError, message, args...)
}

// Fatal writes a fatal message and exits with status 1.
func Fatal(message string, args ...any) {
	log(context.Background(), LevelFatal, message, args...)
	os.Exit(1)
}

func log(ctx context.Context, level slog.Level, message string, args ...any) {
	logger := Default()
	if !logger.Enabled(ctx, level) {
		return
	}

	var pc uintptr
	var tracePCs []uintptr
	if level == LevelTrace {
		// Skip Callers, callerPCs, log, and the public Trace helper.
		tracePCs = callerPCs(4)
		if len(tracePCs) > 0 {
			pc = tracePCs[0]
		}
	} else {
		// Skip Callers, log, and the public package helper. Ordinary records
		// need only their immediate caller, not the complete call stack.
		var pcs [1]uintptr
		if runtime.Callers(3, pcs[:]) == 1 {
			pc = pcs[0]
		}
	}
	record := slog.NewRecord(time.Now(), level, message, pc)
	record.Add(args...)
	if level == LevelTrace {
		record.AddAttrs(slog.Any(traceStackKey, captureTraceStack(tracePCs)))
	}
	_ = logger.Handler().Handle(ctx, record)
}

func callerPCs(skip int) []uintptr {
	pcs := make([]uintptr, 32)
	for {
		count := runtime.Callers(skip, pcs)
		if count < len(pcs) {
			return pcs[:count]
		}
		pcs = make([]uintptr, len(pcs)*2)
	}
}

func captureTraceStack(pcs []uintptr) traceStack {
	var stack strings.Builder
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		if stack.Len() > 0 {
			stack.WriteByte('\n')
		}
		stack.WriteString(frame.Function)
		stack.WriteByte('\n')
		stack.WriteByte('\t')
		stack.WriteString(frame.File)
		stack.WriteByte(':')
		stack.WriteString(strconv.Itoa(frame.Line))
		if !more {
			break
		}
	}
	return traceStack(stack.String())
}
