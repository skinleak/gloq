package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// noTime removes timestamps so expected output can be compared exactly.
var noTime = WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey && len(groups) == 0 {
		return slog.Attr{}
	}
	return attr
})

func TestPrettyOutputCannotForgeLines(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithColor(ColorNever), WithSource(false), noTime))

	logger.Info(
		"user login\n2026-01-01 00:00:00.000 ERROR fake entry",
		"user", "bob\x1b[31mRED",
		"evil key=1", "v",
		"bidi", "a\u202eb",
	)
	logger.Error("failed", "error", errors.New("boom\x1b[2J\nsecond line\rthird"))

	got := output.String()
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\r") || strings.Contains(got, "\u202e") {
		t.Fatalf("output contains control characters: %q", got)
	}
	for _, want := range []string{
		`user login\n2026-01-01 00:00:00.000 ERROR fake entry`,
		`user="bob\x1b[31mRED"`,
		`"evil key=1"=v`,
		`bidi="a\u202eb"`,
		"  error: boom\\x1b[2J\n    second line\\rthird\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q: %q", want, got)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if !strings.HasPrefix(line, "INFO") && !strings.HasPrefix(line, "ERROR") && !strings.HasPrefix(line, "  ") {
			t.Errorf("line could be mistaken for a record: %q", line)
		}
	}
}

func TestErrorKeysAreQuotedOnce(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithColor(ColorNever), WithSource(false), noTime))

	logger.WithGroup("my group").Error("failed", "bad key", errors.New("e"), "plain", errors.New("f"))

	want := "ERROR   failed\n  \"my group.bad key\": e\n  \"my group.plain\": f\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQuotingKeepsReadableValues(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithColor(ColorNever), WithSource(false), noTime))

	logger.Info("héllo wörld", "name", "zoë", "path", "/a/b", "tab", "a\tb", "empty", "", "group.with.dot", 1)

	want := "INFO    héllo wörld name=zoë path=/a/b tab=\"a\\tb\" empty=\"\" group.with.dot=1\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestLevelsAreAligned(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithColor(ColorNever), WithSource(false), noTime, WithLevel(LevelTrace)))
	for _, level := range []slog.Level{LevelTrace, slog.LevelDebug, slog.LevelInfo, LevelSuccess, slog.LevelWarn, slog.LevelError, LevelFatal, slog.LevelError + 1} {
		logger.LogAttrs(context.Background(), level, "message")
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "  ") || line == "" {
			continue // trace stack
		}
		if index := strings.Index(line, "message"); index != levelWidth+1 {
			t.Errorf("message starts at column %d, want %d: %q", index, levelWidth+1, line)
		}
	}
}

func TestColorsDimMetadata(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithColor(ColorAlways), WithSource(false), noTime))

	logger.Info("hello", "key", "value", "error", errors.New("boom"))

	want := infoColor + "INFO" + resetColor + "    hello " +
		faintColor + "key=" + resetColor + "value\n" +
		"  " + errorKeyColor + "error" + resetColor + ": boom\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestForceColor(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{{"1", true}, {"true", true}, {"0", false}, {"false", false}} {
		t.Setenv("NO_COLOR", "")
		t.Setenv("TERM", "xterm")
		t.Setenv("FORCE_COLOR", test.value)
		if got := shouldColor(io.Discard, ColorAuto); got != test.want {
			t.Errorf("FORCE_COLOR=%s: color = %v, want %v", test.value, got, test.want)
		}
	}
}

//go:noinline
func traceThroughLogger(logger *slog.Logger) {
	logger.Log(context.Background(), LevelTrace, "from logger")
}

func inlinedTrace(logger *Logger) {
	logger.Trace("from wrapper")
}

func TestTraceStackForEveryLogger(t *testing.T) {
	t.Run("pretty", func(t *testing.T) {
		var output bytes.Buffer
		logger := slog.New(NewHandler(&output, WithColor(ColorNever), WithLevel(LevelTrace)))

		traceThroughLogger(logger)
		inlinedTrace(Wrap(logger))

		got := output.String()
		for _, want := range []string{
			"TRACE   [features_test.go:",
			"  stack:\n    github.com/skinleak/gloq.traceThroughLogger\n",
			"  stack:\n    github.com/skinleak/gloq.inlinedTrace\n",
			"gloq.TestTraceStackForEveryLogger",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output does not contain %q: %q", want, got)
			}
		}
		if strings.Contains(got, "log/slog.") || strings.Contains(got, "gloq.logAt") {
			t.Fatalf("stack contains logger internals: %q", got)
		}
	})

	t.Run("json", func(t *testing.T) {
		var output bytes.Buffer
		logger := slog.New(NewHandler(&output, WithFormat(FormatJSON), WithLevel(LevelTrace)))

		traceThroughLogger(logger)

		var record struct {
			Stack []traceFrame `json:"stack"`
		}
		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if len(record.Stack) < 2 || record.Stack[0].Function != "github.com/skinleak/gloq.traceThroughLogger" ||
			!strings.HasSuffix(record.Stack[0].File, "features_test.go") || record.Stack[0].Line == 0 {
			t.Fatalf("unexpected stack: %+v", record.Stack)
		}
	})

	t.Run("record without caller", func(t *testing.T) {
		var output bytes.Buffer
		handler := NewHandler(&output, WithColor(ColorNever), WithLevel(LevelTrace))
		if err := handler.Handle(context.Background(), slog.NewRecord(time.Time{}, LevelTrace, "raw", 0)); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); got != "TRACE   raw\n" {
			t.Fatalf("output = %q", got)
		}
	})
}

func TestLoggerFatal(t *testing.T) {
	var codes []int
	previousExit := exit
	exit = func(code int) { codes = append(codes, code) }
	t.Cleanup(func() { exit = previousExit })
	previous := fatalHooks.hooks
	t.Cleanup(func() { fatalHooks.hooks = previous })

	var calls []string
	OnFatal(func() { calls = append(calls, "first") })
	OnFatal(func() { panic("broken hook") })
	OnFatal(func() { calls = append(calls, "last") })

	var output bytes.Buffer
	logger := Wrap(slog.New(NewHandler(&output, WithColor(ColorNever)))).With("service", "api")
	logger.Fatal("stopping", "code", 17)

	if got := output.String(); !strings.Contains(got, "FATAL   [features_test.go:") || !strings.Contains(got, "stopping service=api code=17") {
		t.Fatalf("output = %q", got)
	}
	if len(codes) != 1 || codes[0] != 1 {
		t.Fatalf("exit codes = %v, want [1]", codes)
	}
	if strings.Join(calls, ",") != "first,last" {
		t.Fatalf("hooks = %v", calls)
	}
}

func TestReplaceAttrRedacts(t *testing.T) {
	redact := WithReplaceAttr(func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == "password" {
			return slog.String("password", "[REDACTED]")
		}
		return attr
	})
	for _, format := range []Format{FormatPretty, FormatJSON} {
		var output bytes.Buffer
		logger := slog.New(NewHandler(&output, WithFormat(format), WithSource(false), redact))
		logger.With("password", "bound-secret").Info("login", "password", "hunter2")
		if got := output.String(); strings.Contains(got, "hunter2") || strings.Contains(got, "bound-secret") || !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("secret was not redacted: %q", got)
		}
	}
}

func TestSourceTransformCannotCorruptCache(t *testing.T) {
	mutate := WithReplaceAttr(func(_ []string, attr slog.Attr) slog.Attr {
		if source, ok := attr.Value.Any().(*slog.Source); ok && attr.Key == slog.SourceKey {
			source.File = "/elsewhere/changed.go"
		}
		return attr
	})
	var mutated, plain bytes.Buffer
	mutating := slog.New(NewHandler(&mutated, WithColor(ColorNever), mutate))
	normal := slog.New(NewHandler(&plain, WithColor(ColorNever)))
	for i := 0; i < 2; i++ {
		logger := normal
		if i == 0 {
			logger = mutating
		}
		logger.Info("same call site")
	}
	if !strings.Contains(mutated.String(), "[changed.go:") || !strings.Contains(plain.String(), "[features_test.go:") {
		t.Fatalf("mutated = %q, plain = %q", mutated.String(), plain.String())
	}
}

func TestParseLevel(t *testing.T) {
	for _, test := range []struct {
		text string
		want slog.Level
	}{
		{"trace", LevelTrace}, {"DEBUG", slog.LevelDebug}, {" Info ", slog.LevelInfo},
		{"success", LevelSuccess}, {"warn", slog.LevelWarn}, {"WARNING", slog.LevelWarn},
		{"error", slog.LevelError}, {"fatal", LevelFatal}, {"INFO+1", slog.LevelInfo + 1},
		{"debug-1", slog.LevelDebug - 1}, {"ERROR+92", slog.Level(100)}, {"-4", slog.LevelDebug}, {"12", LevelFatal},
	} {
		got, err := ParseLevel(test.text)
		if err != nil || got != test.want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", test.text, got, err, test.want)
		}
	}
	for _, text := range []string{"", "verbose", "INFO+", "INFO+x", "+1x"} {
		if _, err := ParseLevel(text); err == nil {
			t.Errorf("ParseLevel(%q) did not fail", text)
		}
	}
	// Every name gloq writes can be read back.
	for level := slog.Level(-12); level <= 20; level++ {
		if got, err := ParseLevel(Level(level).String()); err != nil || got != level {
			t.Errorf("round trip of %v (%q) = %v, %v", level, Level(level).String(), got, err)
		}
	}
}

func TestLevelText(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var level Level
	flags.TextVar(&level, "level", Level(slog.LevelInfo), "minimum level")
	if err := flags.Parse([]string{"-level=success"}); err != nil {
		t.Fatal(err)
	}
	if level.Level() != LevelSuccess {
		t.Fatalf("level = %v", level)
	}
	encoded, err := json.Marshal(struct{ Level Level }{Level(LevelTrace)})
	if err != nil || string(encoded) != `{"Level":"TRACE"}` {
		t.Fatalf("json = %s, %v", encoded, err)
	}
	if err := flags.Parse([]string{"-level=loud"}); err == nil {
		t.Fatal("invalid level was accepted")
	}
}

func TestLevelFromEnv(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		t.Setenv("GLOQ_TEST_LEVEL", "debug")
		var output bytes.Buffer
		logger := slog.New(NewHandler(&output, WithSource(false), WithLevelFromEnv("GLOQ_TEST_LEVEL")))
		logger.Debug("visible")
		if !strings.Contains(output.String(), "visible") {
			t.Fatalf("output = %q", output.String())
		}
	})
	t.Run("unset keeps earlier level", func(t *testing.T) {
		var output bytes.Buffer
		logger := slog.New(NewHandler(&output, WithSource(false), WithLevel(slog.LevelWarn), WithLevelFromEnv("GLOQ_TEST_UNSET")))
		logger.Info("hidden")
		if output.Len() != 0 {
			t.Fatalf("output = %q", output.String())
		}
	})
	t.Run("invalid is reported", func(t *testing.T) {
		t.Setenv("GLOQ_TEST_LEVEL", "loud")
		var output bytes.Buffer
		logger := slog.New(NewHandler(&output, WithColor(ColorNever), WithSource(false), noTime, WithLevel(slog.LevelError), WithLevelFromEnv("GLOQ_TEST_LEVEL")))
		logger.Warn("hidden")
		got := output.String()
		if !strings.Contains(got, `WARN    gloq: ignoring GLOQ_TEST_LEVEL="loud"`) || strings.Contains(got, "hidden") {
			t.Fatalf("output = %q", got)
		}
	})
}

type requestIDKey struct{}

func TestContextAttrs(t *testing.T) {
	extract := WithContextAttrs(func(ctx context.Context) []slog.Attr {
		if id, ok := ctx.Value(requestIDKey{}).(string); ok {
			return []slog.Attr{slog.String("request_id", id)}
		}
		return nil
	})
	ctx := context.WithValue(context.Background(), requestIDKey{}, "abc123")

	var pretty bytes.Buffer
	logger := slog.New(NewHandler(&pretty, WithColor(ColorNever), WithSource(false), noTime, extract)).With("service", "api")
	logger.InfoContext(ctx, "handled", "status", 200)
	logger.Info("no context value")
	want := "INFO    handled service=api request_id=abc123 status=200\nINFO    no context value service=api\n"
	if got := pretty.String(); got != want {
		t.Fatalf("pretty = %q, want %q", got, want)
	}

	var jsonOutput bytes.Buffer
	logger = slog.New(NewHandler(&jsonOutput, WithFormat(FormatJSON), WithSource(false), extract))
	logger.InfoContext(ctx, "handled")
	logger.InfoContext(ctx, "again")
	for _, line := range strings.Split(strings.TrimSpace(jsonOutput.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil || record["request_id"] != "abc123" {
			t.Fatalf("record = %s, %v", line, err)
		}
	}
}

func TestPackageContextHelpers(t *testing.T) {
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(&output, WithColor(ColorNever), WithLevel(LevelTrace))))
	t.Cleanup(func() { SetDefault(previous) })
	ctx := context.Background()

	TraceContext(ctx, "trace")
	DebugContext(ctx, "debug")
	InfoContext(ctx, "info")
	SuccessContext(ctx, "success")
	WarnContext(ctx, "warn")
	ErrorContext(ctx, "error")
	With("component", "cache").Success("warmed")

	got := output.String()
	for _, want := range []string{"trace\n", "debug\n", "info\n", "success\n", "warn\n", "error\n", "warmed component=cache\n"} {
		if !strings.Contains(got, "[features_test.go:") || !strings.Contains(got, want) {
			t.Errorf("output does not contain %q: %q", want, got)
		}
	}
	if strings.Contains(got, "[logger.go:") {
		t.Fatalf("caller points into gloq: %q", got)
	}
}

type fakeT struct {
	lines []string
	done  bool
}

func (t *fakeT) Log(args ...any) {
	if t.done {
		panic("Log in goroutine after test has completed")
	}
	t.lines = append(t.lines, args[0].(string))
}

type fakeOutputT struct {
	fakeT
	output bytes.Buffer
}

func (t *fakeOutputT) Output() io.Writer { return &t.output }

func TestNewTestLogger(t *testing.T) {
	fake := &fakeT{}
	logger := NewTestLogger(fake, WithSource(false), noTime)
	logger.Trace("visible")
	if len(fake.lines) != 1 || !strings.HasPrefix(fake.lines[0], "TRACE   visible") || strings.Contains(fake.lines[0], "\x1b") {
		t.Fatalf("lines = %q", fake.lines)
	}

	fake.done = true
	logger.Info("after the test") // must not panic

	withOutput := &fakeOutputT{}
	NewTestLogger(withOutput, WithSource(false)).Info("direct")
	if got := withOutput.output.String(); !strings.HasSuffix(got, "INFO    direct\n") || len(withOutput.lines) != 0 {
		t.Fatalf("output = %q, lines = %q", got, withOutput.lines)
	}

	NewTestLogger(t).Success("works with *testing.T")
}

func TestFormatAuto(t *testing.T) {
	var output bytes.Buffer
	slog.New(NewHandler(&output, WithFormat(FormatAuto))).Info("redirected")
	if !json.Valid(output.Bytes()) {
		t.Fatalf("output is not JSON: %q", output.String())
	}
}

func TestTimeZone(t *testing.T) {
	stamp := time.Date(2026, time.July, 8, 13, 4, 7, 0, time.FixedZone("CEST", 2*60*60))
	record := slog.NewRecord(stamp, slog.LevelInfo, "message", 0)

	var pretty bytes.Buffer
	_ = NewHandler(&pretty, WithColor(ColorNever), WithTimeZone(time.UTC)).Handle(context.Background(), record)
	if got := pretty.String(); !strings.HasPrefix(got, "2026-07-08 11:04:07.000 ") {
		t.Fatalf("pretty = %q", got)
	}

	var jsonOutput bytes.Buffer
	_ = NewHandler(&jsonOutput, WithFormat(FormatJSON), WithTimeZone(time.UTC)).Handle(context.Background(), record)
	if got := jsonOutput.String(); !strings.Contains(got, `"time":"2026-07-08T11:04:07Z"`) {
		t.Fatalf("json = %q", got)
	}
}

func TestSourcePath(t *testing.T) {
	for _, test := range []struct {
		path SourcePath
		want string
	}{
		{SourceFile, "[features_test.go:"},
		{SourceDir, "[gloq/features_test.go:"},
		{SourceFull, "/features_test.go:"},
	} {
		var output bytes.Buffer
		slog.New(NewHandler(&output, WithColor(ColorNever), WithSourcePath(test.path))).Info("message")
		if got := output.String(); !strings.Contains(got, test.want) {
			t.Errorf("path %d: output = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestSiblingLoggersDoNotShareAttrs(t *testing.T) {
	var output bytes.Buffer
	parent := slog.New(NewHandler(&output, WithColor(ColorNever), WithSource(false), noTime)).With("service", "api")
	first := parent.With("worker", 1, "error", errors.New("first"))
	second := parent.With("worker", 2, "error", errors.New("second"))

	first.Info("a")
	second.Info("b")
	parent.Info("c")

	want := "INFO    a service=api worker=1\n  error: first\n" +
		"INFO    b service=api worker=2\n  error: second\n" +
		"INFO    c service=api\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
