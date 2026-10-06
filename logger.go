package gloq

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Logger is a [*slog.Logger] with methods for gloq's own levels. Every slog
// method, such as Info or LogAttrs, is available as usual.
type Logger struct {
	*slog.Logger
}

// NewLogger creates a [*Logger] that writes to standard error.
func NewLogger(options ...Option) *Logger {
	return &Logger{New(options...)}
}

// Wrap adds gloq's level methods to an existing slog logger.
func Wrap(logger *slog.Logger) *Logger {
	if logger == nil {
		panic("gloq: nil logger")
	}
	return &Logger{logger}
}

// With returns a Logger that includes the given attributes in each record.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{l.Logger.With(args...)}
}

// WithGroup returns a Logger that nests the attributes of each record in name.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{l.Logger.WithGroup(name)}
}

// Trace writes a detailed diagnostic message with the current call stack.
func (l *Logger) Trace(message string, args ...any) {
	logAt(context.Background(), l.Logger, LevelTrace, message, args...)
}

// TraceContext is Trace with a context.
func (l *Logger) TraceContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, l.Logger, LevelTrace, message, args...)
}

// Success writes a successful or positive event.
func (l *Logger) Success(message string, args ...any) {
	logAt(context.Background(), l.Logger, LevelSuccess, message, args...)
}

// SuccessContext is Success with a context.
func (l *Logger) SuccessContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, l.Logger, LevelSuccess, message, args...)
}

// Fatal writes a fatal message, runs the [OnFatal] hooks, and exits with
// status 1.
func (l *Logger) Fatal(message string, args ...any) {
	logAt(context.Background(), l.Logger, LevelFatal, message, args...)
	exitFatal()
}

// FatalContext is Fatal with a context.
func (l *Logger) FatalContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, l.Logger, LevelFatal, message, args...)
	exitFatal()
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

// With returns the default logger with the given attributes added.
func With(args ...any) *Logger {
	return &Logger{Default().With(args...)}
}

// Trace writes a detailed diagnostic message with the current call stack.
func Trace(message string, args ...any) {
	logAt(context.Background(), Default(), LevelTrace, message, args...)
}

// Debug writes a debug message.
func Debug(message string, args ...any) {
	logAt(context.Background(), Default(), slog.LevelDebug, message, args...)
}

// Info writes an informational message.
func Info(message string, args ...any) {
	logAt(context.Background(), Default(), slog.LevelInfo, message, args...)
}

// Success writes a successful or positive event.
func Success(message string, args ...any) {
	logAt(context.Background(), Default(), LevelSuccess, message, args...)
}

// Warn writes a warning message.
func Warn(message string, args ...any) {
	logAt(context.Background(), Default(), slog.LevelWarn, message, args...)
}

// Error writes an error message.
func Error(message string, args ...any) {
	logAt(context.Background(), Default(), slog.LevelError, message, args...)
}

// Fatal writes a fatal message, runs the [OnFatal] hooks, and exits with
// status 1.
func Fatal(message string, args ...any) {
	logAt(context.Background(), Default(), LevelFatal, message, args...)
	exitFatal()
}

// TraceContext is Trace with a context.
func TraceContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), LevelTrace, message, args...)
}

// DebugContext is Debug with a context.
func DebugContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), slog.LevelDebug, message, args...)
}

// InfoContext is Info with a context.
func InfoContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), slog.LevelInfo, message, args...)
}

// SuccessContext is Success with a context.
func SuccessContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), LevelSuccess, message, args...)
}

// WarnContext is Warn with a context.
func WarnContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), slog.LevelWarn, message, args...)
}

// ErrorContext is Error with a context.
func ErrorContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), slog.LevelError, message, args...)
}

// FatalContext is Fatal with a context.
func FatalContext(ctx context.Context, message string, args ...any) {
	logAt(ctx, Default(), LevelFatal, message, args...)
	exitFatal()
}

// logAt must be called directly by an exported logging function so the
// record reports that function's caller.
func logAt(ctx context.Context, logger *slog.Logger, level slog.Level, message string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !logger.Enabled(ctx, level) {
		return
	}
	// Skip Callers, logAt, and the exported logging function.
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, message, pcs[0])
	record.Add(args...)
	_ = logger.Handler().Handle(ctx, record)
}

var fatalHooks struct {
	sync.Mutex
	hooks []func()
}

// exit is replaced in tests.
var exit = os.Exit

// OnFatal registers a function that runs after a Fatal record is written and
// before the program exits, such as flushing a buffered writer or closing a
// tracing exporter. Hooks run in the order they were registered; a hook that
// panics does not stop the others.
func OnFatal(hook func()) {
	if hook == nil {
		return
	}
	fatalHooks.Lock()
	defer fatalHooks.Unlock()
	fatalHooks.hooks = append(fatalHooks.hooks, hook)
}

func exitFatal() {
	fatalHooks.Lock()
	hooks := append([]func(){}, fatalHooks.hooks...)
	fatalHooks.Unlock()
	for _, hook := range hooks {
		runFatalHook(hook)
	}
	exit(1)
}

func runFatalHook(hook func()) {
	defer func() { _ = recover() }()
	hook()
}
