package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func decodeLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid JSON %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func TestPresetLevelNames(t *testing.T) {
	levels := []slog.Level{LevelTrace, slog.LevelDebug, slog.LevelInfo, slog.LevelInfo + 1, LevelSuccess, slog.LevelWarn, slog.LevelError, LevelFatal, LevelFatal + 4}
	tests := []struct {
		preset Preset
		key    string
		want   []string
	}{
		{PresetSlog, "level", []string{"TRACE", "DEBUG", "INFO", "INFO+1", "SUCCESS", "WARN", "ERROR", "FATAL", "ERROR+8"}},
		{PresetGCP, "severity", []string{"DEBUG", "DEBUG", "INFO", "INFO", "NOTICE", "WARNING", "ERROR", "CRITICAL", "CRITICAL"}},
		{PresetDatadog, "status", []string{"trace", "debug", "info", "info", "success", "warn", "error", "critical", "critical"}},
	}
	for _, test := range tests {
		// With a ReplaceAttr function, built-in attributes take the slower
		// path through the pipeline, which must give the same result.
		for _, replace := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/replace=%v", test.preset, replace), func(t *testing.T) {
				var output bytes.Buffer
				options := []Option{WithFormat(FormatJSON), WithPreset(test.preset), WithLevel(LevelTrace), WithSource(false)}
				if replace {
					options = append(options, WithReplaceAttr(func(_ []string, attr slog.Attr) slog.Attr { return attr }))
				}
				logger := slog.New(NewHandler(&output, options...))
				for _, level := range levels {
					logger.Log(context.Background(), level, "message")
				}
				for index, record := range decodeLines(t, output.String()) {
					if record[test.key] != test.want[index] {
						t.Errorf("level %v: %s = %v, want %q", levels[index], test.key, record[test.key], test.want[index])
					}
				}
			})
		}
	}
}

func TestPresetGCP(t *testing.T) {
	for _, replace := range []bool{false, true} {
		var output bytes.Buffer
		options := []Option{WithFormat(FormatJSON), WithPreset(PresetGCP)}
		if replace {
			options = append(options, WithReplaceAttr(func(_ []string, attr slog.Attr) slog.Attr { return attr }))
		}
		slog.New(NewHandler(&output, options...)).Warn("disk almost full", "free", "2%")
		record := decodeLines(t, output.String())[0]
		for _, key := range []string{"level", "msg", "source"} {
			if _, ok := record[key]; ok {
				t.Errorf("replace=%v: record has slog key %q: %v", replace, key, record)
			}
		}
		if record["severity"] != "WARNING" || record["message"] != "disk almost full" || record["free"] != "2%" || record["time"] == nil {
			t.Errorf("replace=%v: record = %v", replace, record)
		}
		location, _ := record["logging.googleapis.com/sourceLocation"].(map[string]any)
		if file, _ := location["file"].(string); !strings.HasSuffix(file, "preset_test.go") || location["line"] == nil || location["function"] == nil {
			t.Errorf("replace=%v: sourceLocation = %v", replace, location)
		}
	}
}

func TestPresetDatadog(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, WithFormat(FormatJSON), WithPreset(PresetDatadog), WithSource(false)))
	logger.Error("payment failed", "error", fmt.Errorf("charge: %w", errors.New("card declined")))
	record := decodeLines(t, output.String())[0]
	if record["status"] != "error" || record["message"] != "payment failed" || record["timestamp"] == nil {
		t.Fatalf("record = %v", record)
	}
	err, _ := record["error"].(map[string]any)
	causes, _ := err["causes"].([]any)
	if err["message"] != "charge: card declined" || err["kind"] != "*fmt.wrapError" || len(causes) != 1 || causes[0].(map[string]any)["kind"] != "*errors.errorString" {
		t.Fatalf("error = %v", err)
	}
	if _, ok := err["type"]; ok {
		t.Fatalf("error has a type field: %v", err)
	}
}

func TestPresetReplaceAttr(t *testing.T) {
	var output bytes.Buffer
	var seen []string
	logger := slog.New(NewHandler(&output, WithFormat(FormatJSON), WithPreset(PresetGCP), WithSource(false), WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
		seen = append(seen, attr.Key)
		if len(groups) == 0 && attr.Key == slog.TimeKey {
			// A key chosen by ReplaceAttr is kept.
			return slog.String("ts", "now")
		}
		return attr
	})))
	// A record's own attributes are never renamed, even with a built-in
	// key or a level value.
	logger.Info("hello", "level", slog.LevelWarn, "msg", "own")

	if want := "time level msg level msg"; strings.Join(seen, " ") != want {
		t.Errorf("ReplaceAttr saw %q, want %q", strings.Join(seen, " "), want)
	}
	want := `{"ts":"now","severity":"INFO","message":"hello","level":"WARN","msg":"own"}` + "\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %s, want %s", got, want)
	}
}

func TestPresetDoesNotChangePretty(t *testing.T) {
	var plain, preset bytes.Buffer
	slog.New(NewHandler(&plain, WithColor(ColorNever), WithSource(false), noTime)).Warn("careful", "error", errors.New("boom"))
	slog.New(NewHandler(&preset, WithColor(ColorNever), WithSource(false), noTime, WithPreset(PresetGCP))).Warn("careful", "error", errors.New("boom"))
	if plain.String() != preset.String() {
		t.Fatalf("pretty with preset = %q, want %q", preset.String(), plain.String())
	}
}

func TestUnknownPreset(t *testing.T) {
	var plain, unknown bytes.Buffer
	slog.New(NewHandler(&plain, WithFormat(FormatJSON), WithSource(false), noTime)).Info("hi")
	slog.New(NewHandler(&unknown, WithFormat(FormatJSON), WithSource(false), noTime, WithPreset(Preset(200)))).Info("hi")
	if plain.String() != unknown.String() {
		t.Fatalf("unknown preset = %s, want %s", unknown.String(), plain.String())
	}
}
