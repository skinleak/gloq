package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

func TestPrettyHandler(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(
		&output,
		WithColor(ColorNever),
		WithLevel(slog.LevelDebug),
	)
	logger := slog.New(handler).With("service", "api").WithGroup("request")

	record := slog.NewRecord(
		time.Date(2026, time.July, 8, 11, 4, 7, 312_000_000, time.UTC),
		slog.LevelInfo,
		"request complete",
		0,
	)
	record.Add("status", 200, "error", errors.New("none found"))
	if err := logger.Handler().Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	want := "2026-07-08 11:04:07.312 INFO  request complete service=api request.status=200\n  request.error: none found\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestHandlerFiltersLevels(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithLevel(slog.LevelWarn)))

	logger.Info("hidden")
	logger.Warn("visible")

	if strings.Contains(output.String(), "hidden") {
		t.Fatalf("disabled record was written: %q", output.String())
	}
	if !strings.Contains(output.String(), "visible") {
		t.Fatalf("enabled record was not written: %q", output.String())
	}
}

func TestPrettyBuiltInLevels(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		label string
	}{
		{name: "trace", level: LevelTrace, label: "TRACE"},
		{name: "debug", level: slog.LevelDebug, label: "DEBUG"},
		{name: "info", level: slog.LevelInfo, label: "INFO"},
		{name: "info plus one", level: slog.LevelInfo + 1, label: "INFO+1"},
		{name: "success", level: LevelSuccess, label: "SUCCESS"},
		{name: "info plus three", level: slog.LevelInfo + 3, label: "INFO+3"},
		{name: "warn", level: slog.LevelWarn, label: "WARN"},
		{name: "error", level: slog.LevelError, label: "ERROR"},
		{name: "error plus one", level: slog.LevelError + 1, label: "ERROR+1"},
		{name: "fatal", level: LevelFatal, label: "FATAL"},
		{name: "fatal plus one", level: LevelFatal + 1, label: "ERROR+5"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(
				&output,
				WithColor(ColorNever),
				WithLevel(LevelTrace),
				WithSource(false),
			)
			record := slog.NewRecord(time.Time{}, test.level, "message", 0)

			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			padding := strings.Repeat(" ", max(0, 5-len(test.label)))
			if got, want := output.String(), test.label+padding+" message\n"; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestSuccessLevelFiltering(t *testing.T) {
	tests := []struct {
		name    string
		minimum slog.Level
		visible bool
	}{
		{name: "info", minimum: slog.LevelInfo, visible: true},
		{name: "success", minimum: LevelSuccess, visible: true},
		{name: "warn", minimum: slog.LevelWarn, visible: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(NewHandler(
				&output,
				WithLevel(test.minimum),
				WithSource(false),
			))

			logger.Log(context.Background(), LevelSuccess, "operation completed")

			if got := output.Len() > 0; got != test.visible {
				t.Fatalf("output visible = %v, want %v; output = %q", got, test.visible, output.String())
			}
		})
	}
}

func TestDynamicLevelFiltering(t *testing.T) {
	var output bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelWarn)
	logger := slog.New(NewHandler(
		&output,
		WithLevel(&level),
		WithSource(false),
	))

	logger.Log(context.Background(), LevelSuccess, "hidden success")
	if output.Len() != 0 {
		t.Fatalf("success was written at WARN minimum: %q", output.String())
	}

	level.Set(LevelSuccess)
	logger.Log(context.Background(), slog.LevelInfo, "hidden info")
	logger.Log(context.Background(), LevelSuccess, "visible success")
	if got := output.String(); strings.Contains(got, "hidden info") || !strings.Contains(got, "visible success") {
		t.Fatalf("unexpected output at SUCCESS minimum: %q", got)
	}

	output.Reset()
	level.Set(LevelTrace)
	logger.Log(context.Background(), LevelTrace, "visible trace")
	if got := output.String(); !strings.Contains(got, "visible trace") {
		t.Fatalf("trace was not written after lowering minimum: %q", got)
	}
}

func TestJSONHandler(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(
		&output,
		WithFormat(FormatJSON),
		WithSource(false),
	))

	logger.Info("hello", "answer", 42)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if record["msg"] != "hello" || record["answer"] != float64(42) {
		t.Fatalf("unexpected JSON record: %#v", record)
	}
}

func TestJSONSuccessLevel(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(
		&output,
		WithFormat(FormatJSON),
		WithSource(false),
	))

	logger.Log(context.Background(), LevelSuccess, "operation completed")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if record[slog.LevelKey] != "SUCCESS" {
		t.Fatalf("level = %v, want SUCCESS", record[slog.LevelKey])
	}
}

func TestJSONCustomLevelNames(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		want  string
	}{
		{name: "info plus one", level: slog.LevelInfo + 1, want: "INFO+1"},
		{name: "success", level: LevelSuccess, want: "SUCCESS"},
		{name: "info plus three", level: slog.LevelInfo + 3, want: "INFO+3"},
		{name: "error plus one", level: slog.LevelError + 1, want: "ERROR+1"},
		{name: "fatal", level: LevelFatal, want: "FATAL"},
		{name: "fatal plus one", level: LevelFatal + 1, want: "ERROR+5"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(NewHandler(
				&output,
				WithFormat(FormatJSON),
				WithSource(false),
			))

			logger.Log(context.Background(), test.level, "message")

			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatalf("output is not JSON: %v", err)
			}
			if got := record[slog.LevelKey]; got != test.want {
				t.Fatalf("level = %v, want %s", got, test.want)
			}
		})
	}
}

func TestPackageFunctionReportsCaller(t *testing.T) {
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(&output, WithColor(ColorNever))))
	t.Cleanup(func() { SetDefault(previous) })

	Info("hello")

	if got := output.String(); !strings.Contains(got, "[gloq_test.go:") {
		t.Fatalf("output does not contain caller: %q", got)
	}
}

func TestSuccessReportsCallerAndAttributes(t *testing.T) {
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(&output, WithColor(ColorNever))))
	t.Cleanup(func() { SetDefault(previous) })

	Success("operation completed", "job_id", 42)

	got := output.String()
	for _, want := range []string{
		"SUCCESS",
		"[gloq_test.go:",
		"operation completed job_id=42",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q: %q", want, got)
		}
	}
	if strings.Contains(got, "  stack:") {
		t.Fatalf("success output contains a trace stack: %q", got)
	}
}

func TestTraceIncludesCallStack(t *testing.T) {
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(
		&output,
		WithColor(ColorNever),
		WithLevel(LevelTrace),
	)))
	t.Cleanup(func() { SetDefault(previous) })

	Trace("following execution", "request_id", "abc123")

	got := output.String()
	for _, want := range []string{
		"TRACE ",
		"following execution request_id=abc123\n",
		"  stack:\n",
		"gloq.TestTraceIncludesCallStack",
		"gloq_test.go:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q: %q", want, got)
		}
	}
	if strings.Contains(got, "gloq.Trace\n") || strings.Contains(got, "gloq.log\n") {
		t.Fatalf("output contains logger internals: %q", got)
	}
}

func TestTraceJSON(t *testing.T) {
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(
		&output,
		WithFormat(FormatJSON),
		WithSource(false),
		WithLevel(LevelTrace),
	)))
	t.Cleanup(func() { SetDefault(previous) })

	Trace("following execution")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if record[slog.LevelKey] != "TRACE" {
		t.Fatalf("level = %v, want TRACE", record[slog.LevelKey])
	}
	stack, ok := record[traceStackKey].(string)
	if !ok || !strings.Contains(stack, "gloq.TestTraceJSON") {
		t.Fatalf("stack = %#v", record[traceStackKey])
	}
}

func TestTraceIsDisabledByDefault(t *testing.T) {
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(&output)))
	t.Cleanup(func() { SetDefault(previous) })

	Trace("hidden")

	if output.Len() != 0 {
		t.Fatalf("disabled trace was written: %q", output.String())
	}
}

func TestColorAlways(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		label string
		color string
	}{
		{name: "trace", level: LevelTrace, label: "TRACE", color: traceColor},
		{name: "debug", level: slog.LevelDebug, label: "DEBUG", color: debugColor},
		{name: "info", level: slog.LevelInfo, label: "INFO", color: infoColor},
		{name: "success", level: LevelSuccess, label: "SUCCESS", color: successColor},
		{name: "warn", level: slog.LevelWarn, label: "WARN", color: warnColor},
		{name: "error", level: slog.LevelError, label: "ERROR", color: errorColor},
		{name: "fatal", level: LevelFatal, label: "FATAL", color: fatalColor},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(NewHandler(
				&output,
				WithColor(ColorAlways),
				WithLevel(LevelTrace),
				WithSource(false),
			))

			logger.Log(context.Background(), test.level, "hello")

			if got := output.String(); !strings.Contains(got, test.color+test.label+resetColor) {
				t.Fatalf("output does not contain expected color: %q", got)
			}
			if test.level == slog.LevelDebug && strings.Contains(output.String(), successColor+"DEBUG") {
				t.Fatalf("debug output uses success green: %q", output.String())
			}
		})
	}
}

func TestPrettyHandlerConformance(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(
		&output,
		WithColor(ColorNever),
		WithSource(false),
		WithTimeFormat(time.RFC3339Nano),
	)

	err := slogtest.TestHandler(handler, func() []map[string]any {
		lines := strings.Split(strings.TrimSpace(output.String()), "\n")
		results := make([]map[string]any, 0, len(lines))
		for _, line := range lines {
			results = append(results, parsePrettyRecord(t, line))
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestJSONFatalLevel(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(&output, WithFormat(FormatJSON), WithSource(false))
	record := slog.NewRecord(time.Now(), LevelFatal, "stopping", 0)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if result[slog.LevelKey] != "FATAL" {
		t.Fatalf("level = %v, want FATAL", result[slog.LevelKey])
	}
}

func TestFatal(t *testing.T) {
	if mode := os.Getenv("GLOQ_FATAL_TEST"); mode != "" {
		options := []Option{WithColor(ColorNever), WithSource(false)}
		if mode == "filtered" {
			options = append(options, WithLevel(slog.Level(20)))
		}
		SetDefault(slog.New(NewHandler(os.Stderr, options...)))
		Fatal("fatal message", "code", 17)
		fmt.Fprintln(os.Stderr, "after Fatal")
		return
	}

	tests := []struct {
		name       string
		mode       string
		wantRecord bool
	}{
		{name: "emitted", mode: "enabled", wantRecord: true},
		{name: "filtered", mode: "filtered", wantRecord: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestFatal$")
			command.Env = append(os.Environ(), "GLOQ_FATAL_TEST="+test.mode)
			output, err := command.CombinedOutput()
			exitError, ok := err.(*exec.ExitError)
			if !ok || exitError.ExitCode() != 1 {
				t.Fatalf("exit error = %v, output = %q", err, output)
			}
			if strings.Contains(string(output), "after Fatal") {
				t.Fatalf("code after Fatal ran: %q", output)
			}
			hasRecord := strings.Contains(string(output), "FATAL fatal message code=17")
			if hasRecord != test.wantRecord {
				t.Fatalf("record present = %v, want %v; output = %q", hasRecord, test.wantRecord, output)
			}
		})
	}
}

func TestWriterErrors(t *testing.T) {
	want := errors.New("write failed")

	for _, format := range []struct {
		name   string
		option Option
	}{
		{name: "pretty", option: WithFormat(FormatPretty)},
		{name: "json", option: WithFormat(FormatJSON)},
	} {
		t.Run(format.name+" handler returns error", func(t *testing.T) {
			writer := &errorWriter{err: want}
			handler := NewHandler(writer, format.option, WithSource(false))
			record := slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0)

			if got := handler.Handle(context.Background(), record); !errors.Is(got, want) {
				t.Fatalf("Handle() error = %v, want %v", got, want)
			}
			if writer.calls != 1 {
				t.Fatalf("writer calls = %d, want 1", writer.calls)
			}
		})
	}

	t.Run("package helper cannot return error", func(t *testing.T) {
		writer := &errorWriter{err: want}
		previous := Default()
		SetDefault(slog.New(NewHandler(writer, WithSource(false))))
		t.Cleanup(func() { SetDefault(previous) })

		Info("message")

		if writer.calls != 1 {
			t.Fatalf("writer calls = %d, want 1", writer.calls)
		}
	})
}

func TestConcurrentWritesStaySeparate(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithSource(false)))
	loggers := []*slog.Logger{
		logger,
		logger.With("service", "api"),
		logger.WithGroup("request"),
	}

	const writers = 20
	var wait sync.WaitGroup
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(id int) {
			defer wait.Done()
			loggers[id%len(loggers)].Info("message", "id", id)
		}(i)
	}
	wait.Wait()

	if lines := strings.Count(output.String(), "\n"); lines != writers {
		t.Fatalf("got %d complete lines, want %d", lines, writers)
	}
}

func TestPrettyErrorChain(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithSource(false)))
	err := fmt.Errorf("query failed: %w", errors.New("connection refused"))

	logger.Error("could not load user", "attempt", 3, "error", err)

	got := output.String()
	for _, want := range []string{
		"could not load user attempt=3\n",
		"  error: query failed\n",
		"    caused by: connection refused\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q: %q", want, got)
		}
	}
}

func TestPrettyJoinedErrors(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithSource(false)))
	err := errors.Join(errors.New("cache unavailable"), errors.New("database unavailable"))

	logger.Error("startup failed", "error", err)

	got := output.String()
	for _, want := range []string{
		"error: 2 errors",
		"caused by[0]: cache unavailable",
		"caused by[1]: database unavailable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q: %q", want, got)
		}
	}
}

func TestStructuredJSONError(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(
		&output,
		WithFormat(FormatJSON),
		WithSource(false),
	))
	err := fmt.Errorf("query failed: %w", errors.New("connection refused"))

	logger.Error("could not load user", "error", err)

	var record struct {
		Error structuredError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if record.Error.Message != "query failed: connection refused" {
		t.Fatalf("message = %q", record.Error.Message)
	}
	if len(record.Error.Causes) != 1 || record.Error.Causes[0].Message != "connection refused" {
		t.Fatalf("causes = %#v", record.Error.Causes)
	}
}

func TestTypedNilError(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithSource(false)))
	var err *nilError

	logger.Error("failed", "error", err)

	if got := output.String(); !strings.Contains(got, "error: <nil>") {
		t.Fatalf("output = %q", got)
	}
}

func TestOptionalErrorStack(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(
		&output,
		WithSource(false),
		WithErrorStack(true),
	))

	logger.Error("failed", "error", stackError{})

	got := output.String()
	if !strings.Contains(got, "stack: boom\n      example.go:12") {
		t.Fatalf("output does not contain the stack: %q", got)
	}
}

func TestPathologicalErrorsDoNotPanic(t *testing.T) {
	cycle := &cyclicError{}
	tests := []struct {
		name string
		err  error
	}{
		{name: "panicking Error", err: panickingError{}},
		{name: "panicking Unwrap", err: panickingUnwrapper{}},
		{name: "cyclic Unwrap", err: cycle},
		{name: "nil multi cause", err: multiError{causes: []error{nil, errors.New("cause"), nil}}},
	}
	formats := []struct {
		name   string
		option Option
	}{
		{name: "pretty", option: WithFormat(FormatPretty)},
		{name: "json", option: WithFormat(FormatJSON)},
	}

	for _, format := range formats {
		for _, test := range tests {
			t.Run(format.name+"/"+test.name, func(t *testing.T) {
				var output bytes.Buffer
				logger := slog.New(NewHandler(&output, format.option, WithSource(false)))

				logger.Error("failed", "error", test.err)

				if output.Len() == 0 {
					t.Fatal("no output was written")
				}
				if test.name == "panicking Error" && !strings.Contains(output.String(), "<error message panicked>") {
					t.Fatalf("panic sentinel is missing: %q", output.String())
				}
				if format.name == "json" && !json.Valid(output.Bytes()) {
					t.Fatalf("output is not valid JSON: %q", output.String())
				}
			})
		}
	}
}

func TestNilMultiErrorCausesAreFiltered(t *testing.T) {
	detail := describeError(
		multiError{causes: []error{nil, errors.New("cause"), nil}},
		false,
		0,
	)
	if len(detail.Causes) != 1 || detail.Causes[0].Message != "cause" {
		t.Fatalf("causes = %#v", detail.Causes)
	}
}

func TestErrorDepthIsBounded(t *testing.T) {
	var err error = errors.New("root")
	for i := 0; i < maxErrorDepth+10; i++ {
		err = fmt.Errorf("layer %d: %w", i, err)
	}

	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithFormat(FormatJSON), WithSource(false)))
	logger.Error("failed", "error", err)

	var record struct {
		Error structuredError `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	depth := 1
	for detail := record.Error; len(detail.Causes) > 0; detail = detail.Causes[0] {
		depth++
	}
	if depth != maxErrorDepth+1 {
		t.Fatalf("error depth = %d, want %d", depth, maxErrorDepth+1)
	}
}

func TestMultiplePrettyErrors(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithSource(false)))

	logger.Error("failed", "first", errors.New("one"), "second", errors.New("two"))

	got := output.String()
	if strings.Count(got, "first: one") != 1 || strings.Count(got, "second: two") != 1 {
		t.Fatalf("unexpected error output: %q", got)
	}
}

func TestRecursiveLogValuerIsBounded(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithSource(false)))

	logger.Info("message", "value", recursiveLogValuer{})

	if got := output.String(); !strings.Contains(got, "LogValue called too many times") {
		t.Fatalf("output does not contain resolution error: %q", got)
	}
}

func TestAttributePipelineMatchesFormats(t *testing.T) {
	transform := withAttrTransform(func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.MessageKey {
			attr.Value = slog.StringValue("changed")
		}
		if attr.Key == "secret" {
			return slog.Attr{}
		}
		if attr.Key == "id" && len(groups) == 1 && groups[0] == "request" {
			attr.Key = "request_id"
		}
		return attr
	})

	var pretty bytes.Buffer
	slog.New(NewHandler(&pretty, WithSource(false), transform)).Info(
		"handled",
		slog.Group("request", "id", "abc", "secret", "hidden"),
	)
	if got := pretty.String(); !strings.Contains(got, "changed request.request_id=abc") || strings.Contains(got, "hidden") {
		t.Fatalf("unexpected pretty output: %q", got)
	}

	var jsonOutput bytes.Buffer
	slog.New(NewHandler(&jsonOutput, WithFormat(FormatJSON), WithSource(false), transform)).Info(
		"handled",
		slog.Group("request", "id", "abc", "secret", "hidden"),
	)
	var record map[string]any
	if err := json.Unmarshal(jsonOutput.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	request, ok := record["request"].(map[string]any)
	if !ok || request["request_id"] != "abc" {
		t.Fatalf("unexpected JSON output: %#v", record)
	}
	if _, exists := request["secret"]; exists {
		t.Fatalf("secret was not removed: %#v", request)
	}
	if record[slog.MessageKey] != "changed" {
		t.Fatalf("message was not transformed: %#v", record)
	}
}

func TestSourceTransformStillApplies(t *testing.T) {
	transform := withAttrTransform(func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.SourceKey {
			attr.Value = slog.StringValue("redacted")
		}
		return attr
	})
	var output bytes.Buffer
	previous := Default()
	SetDefault(slog.New(NewHandler(&output, WithColor(ColorNever), transform)))
	t.Cleanup(func() { SetDefault(previous) })

	Info("message")

	if got := output.String(); !strings.Contains(got, "[redacted] message") {
		t.Fatalf("source transform was not applied: %q", got)
	}
}

type nilError struct{}

func (e *nilError) Error() string {
	if e == nil {
		panic("nil error")
	}
	return "error"
}

type stackError struct{}

func (stackError) Error() string { return "boom" }

func (stackError) Format(state fmt.State, verb rune) {
	if verb == 'v' && state.Flag('+') {
		fmt.Fprint(state, "boom\nexample.go:12")
		return
	}
	fmt.Fprint(state, "boom")
}

type errorWriter struct {
	err   error
	calls int
}

func (w *errorWriter) Write(_ []byte) (int, error) {
	w.calls++
	return 0, w.err
}

type panickingError struct{}

func (panickingError) Error() string { panic("boom") }

type panickingUnwrapper struct{}

func (panickingUnwrapper) Error() string { return "outer" }
func (panickingUnwrapper) Unwrap() error { panic("boom") }

type cyclicError struct{}

func (e *cyclicError) Error() string { return "cycle" }
func (e *cyclicError) Unwrap() error { return e }

type multiError struct {
	causes []error
}

func (e multiError) Error() string   { return "multiple" }
func (e multiError) Unwrap() []error { return e.causes }

type recursiveLogValuer struct{}

func (recursiveLogValuer) LogValue() slog.Value {
	return slog.AnyValue(recursiveLogValuer{})
}

func FuzzPrettyHandler(f *testing.F) {
	f.Add("message", "request", "id", "abc123", int32(slog.LevelInfo))
	f.Add("spaces and \"quotes\"", "", "key.with.dot", "a=b\nnext", int32(LevelSuccess))

	f.Fuzz(func(t *testing.T, message, group, key, value string, rawLevel int32) {
		var output bytes.Buffer
		handler := NewHandler(
			&output,
			WithColor(ColorNever),
			WithLevel(LevelTrace),
			WithSource(false),
		)
		record := slog.NewRecord(time.Time{}, slog.Level(rawLevel), message, 0)
		record.AddAttrs(slog.Group(group, slog.String(key, value)))

		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if got := output.String(); !strings.HasSuffix(got, "\n") {
			t.Fatalf("output is not newline terminated: %q", got)
		}
	})
}

func parsePrettyRecord(t *testing.T, line string) map[string]any {
	t.Helper()
	fields := strings.Fields(line)
	if len(fields) < 2 {
		t.Fatalf("invalid pretty record: %q", line)
	}

	result := make(map[string]any)
	index := 0
	if _, err := time.Parse(time.RFC3339Nano, fields[index]); err == nil {
		result[slog.TimeKey] = fields[index]
		index++
	}
	if len(fields) < index+2 {
		t.Fatalf("invalid pretty record: %q", line)
	}
	result[slog.LevelKey] = fields[index]
	result[slog.MessageKey] = fields[index+1]
	index += 2

	for _, field := range fields[index:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("invalid field %q in %q", field, line)
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		setNested(result, strings.Split(key, "."), value)
	}
	return result
}

func setNested(record map[string]any, path []string, value any) {
	for _, part := range path[:len(path)-1] {
		next, ok := record[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			record[part] = next
		}
		record = next
	}
	record[path[len(path)-1]] = value
}
