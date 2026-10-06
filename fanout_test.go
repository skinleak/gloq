package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"
	"time"
)

func TestFanoutWritesToEveryEnabledHandler(t *testing.T) {
	var pretty, jsonOutput bytes.Buffer
	logger := slog.New(Fanout(
		NewHandler(&pretty, WithColor(ColorNever), WithLevel(slog.LevelDebug)),
		nil,
		NewHandler(&jsonOutput, WithFormat(FormatJSON), WithLevel(slog.LevelWarn)),
	)).With("service", "api").WithGroup("request")

	logger.Debug("debug only")
	logger.Warn("both", "status", 503)

	if got := pretty.String(); !strings.Contains(got, "debug only") || !strings.Contains(got, "both service=api request.status=503") {
		t.Fatalf("pretty output = %q", got)
	}
	if strings.Contains(jsonOutput.String(), "debug only") {
		t.Fatalf("JSON handler got a disabled record: %q", jsonOutput.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(jsonOutput.Bytes(), &entry); err != nil {
		t.Fatalf("JSON output %q: %v", jsonOutput.String(), err)
	}
	request, _ := entry["request"].(map[string]any)
	if entry["service"] != "api" || request["status"] != float64(503) {
		t.Fatalf("JSON entry = %v", entry)
	}
}

func TestFanoutEnabled(t *testing.T) {
	handler := Fanout(
		NewHandler(&bytes.Buffer{}, WithLevel(slog.LevelError)),
		NewHandler(&bytes.Buffer{}, WithLevel(slog.LevelWarn)),
	)
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("INFO is enabled although no handler accepts it")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("WARN is disabled although one handler accepts it")
	}
	if Fanout().Enabled(context.Background(), slog.LevelError) {
		t.Fatal("an empty fanout is enabled")
	}
}

func TestFanoutJoinsErrors(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	var output bytes.Buffer
	logger := slog.New(Fanout(
		NewHandler(&errorWriter{err: first}),
		NewHandler(&output),
		NewHandler(&errorWriter{err: second}),
	))
	err := logger.Handler().Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "message", 0))
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("Handle() error = %v, want both writer errors", err)
	}
	if !strings.Contains(output.String(), "message") {
		t.Fatal("a failing handler stopped the others")
	}
}

func TestFanoutRecordsAreIndependent(t *testing.T) {
	var first, second bytes.Buffer
	logger := slog.New(Fanout(
		NewHandler(&first, WithColor(ColorNever), WithContextAttrs(func(context.Context) []slog.Attr {
			return []slog.Attr{slog.String("from", "context")}
		})),
		NewHandler(&second, WithColor(ColorNever)),
	))
	logger.Info("message", "a", 1, "b", 2, "c", 3, "d", 4, "e", 5, "f", 6)
	if strings.Contains(second.String(), "from=context") {
		t.Fatalf("an attribute added by one handler reached another: %q", second.String())
	}
	if !strings.Contains(second.String(), "f=6") {
		t.Fatalf("second output = %q", second.String())
	}
}

func TestFanoutTraceStack(t *testing.T) {
	var output bytes.Buffer
	logger := Wrap(slog.New(Fanout(NewHandler(&output, WithColor(ColorNever), WithLevel(LevelTrace)))))
	logger.Trace("deep")
	if !strings.Contains(output.String(), "TestFanoutTraceStack") {
		t.Fatalf("TRACE stack does not start at the caller: %q", output.String())
	}
}

func TestFanoutConformance(t *testing.T) {
	var output bytes.Buffer
	handler := Fanout(NewHandler(&output, WithFormat(FormatJSON), WithSource(false)))
	err := slogtest.TestHandler(handler, func() []map[string]any {
		var results []map[string]any
		for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
			var entry map[string]any
			if err := json.Unmarshal(line, &entry); err != nil {
				t.Fatal(err)
			}
			results = append(results, entry)
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}
