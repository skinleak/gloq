package gloqtest_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"

	"github.com/skinleak/gloq"
	"github.com/skinleak/gloq/gloqtest"
)

func TestSlogtest(t *testing.T) {
	rec := gloqtest.NewRecorder()
	err := slogtest.TestHandler(rec, func() []map[string]any {
		var results []map[string]any
		for _, record := range rec.Records() {
			result := record.Map()
			if !record.Time.IsZero() {
				result[slog.TimeKey] = record.Time
			}
			result[slog.LevelKey] = record.Level
			result[slog.MessageKey] = record.Message
			results = append(results, result)
		}
		return results
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fakeT records assertion failures instead of failing the test.
type fakeT struct {
	failures []string
}

func (t *fakeT) Helper() {}

func (t *fakeT) Errorf(format string, args ...any) {
	t.failures = append(t.failures, fmt.Sprintf(format, args...))
}

func TestAssertLogged(t *testing.T) {
	rec := gloqtest.NewRecorder()
	log := rec.Logger().With("service", "api").WithGroup("http")
	log.Info("request", "status", 200, "path", "/orders")
	log.Error("request failed", "status", 500, "error", fmt.Errorf("read body: %w", fs.ErrClosed))
	rec.Logger().Success("cache warmed", "entries", int64(128), "tags", []string{"a", "b"})

	rec.AssertLogged(t, slog.LevelInfo, "request")
	rec.AssertLogged(t, slog.LevelInfo, "request", "service", "api", slog.Group("http", "status", 200))
	rec.AssertLogged(t, slog.LevelError, "request failed", slog.Group("http", "error", fs.ErrClosed))
	rec.AssertLogged(t, gloq.LevelSuccess, "cache warmed", "entries", 128, "tags", []string{"a", "b"})
	rec.AssertNotLogged(t, slog.LevelWarn, "request")
	rec.AssertNotLogged(t, slog.LevelInfo, "request", slog.Group("http", "status", 404))

	record := rec.AssertLogged(t, slog.LevelInfo, "request")
	if value, ok := record.Value("http.path"); !ok || value.String() != "/orders" {
		t.Errorf("Value(http.path) = %v, %v", value, ok)
	}
	if _, ok := record.Value("http.missing"); ok {
		t.Error("Value(http.missing) was found")
	}
	if source := record.Source(); source == nil || !strings.HasSuffix(source.File, "gloqtest_test.go") {
		t.Errorf("Source() = %v, want this file", source)
	}
	if got := rec.Count(slog.LevelError, "request failed"); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
}

func TestAssertionFailures(t *testing.T) {
	rec := gloqtest.NewRecorder()
	fake := &fakeT{}
	rec.AssertLogged(fake, slog.LevelInfo, "anything")
	if len(fake.failures) != 1 || !strings.Contains(fake.failures[0], "nothing was logged") {
		t.Fatalf("failures = %q", fake.failures)
	}

	rec.Logger().Warn("disk almost full", "free", "2%", slog.Group("disk", "name", "sda"))
	fake = &fakeT{}
	rec.AssertLogged(fake, slog.LevelWarn, "disk almost full", "free", "5%")
	rec.AssertNotLogged(fake, slog.LevelWarn, "disk almost full")
	want := []string{
		"no record matches WARN \"disk almost full\" free=\"5%\"\n1 records were logged:\n\tWARN \"disk almost full\" free=\"2%\" disk.name=\"sda\"",
		"unexpected record matches WARN \"disk almost full\":\n\tWARN \"disk almost full\" free=\"2%\" disk.name=\"sda\"",
	}
	if strings.Join(fake.failures, "\n--\n") != strings.Join(want, "\n--\n") {
		t.Fatalf("failures =\n%s\nwant\n%s", strings.Join(fake.failures, "\n--\n"), strings.Join(want, "\n--\n"))
	}
}

func TestErrorsMatchWithIs(t *testing.T) {
	rec := gloqtest.NewRecorder()
	rec.Logger().Error("failed", "error", fmt.Errorf("wrapped: %w", fs.ErrNotExist))
	rec.AssertLogged(t, slog.LevelError, "failed", "error", fs.ErrNotExist)
	rec.AssertNotLogged(t, slog.LevelError, "failed", "error", errors.New("other"))
}

type token string

func (token) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

func TestValuesAreResolved(t *testing.T) {
	rec := gloqtest.NewRecorder()
	rec.Logger().Info("login", "token", token("secret"))
	rec.AssertLogged(t, slog.LevelInfo, "login", "token", "[REDACTED]")
}

func TestReset(t *testing.T) {
	rec := gloqtest.NewRecorder()
	rec.Logger().Info("first")
	rec.Reset()
	if records := rec.Records(); len(records) != 0 {
		t.Fatalf("Records() after Reset = %v", records)
	}
}

func TestConcurrent(t *testing.T) {
	rec := gloqtest.NewRecorder()
	log := slog.New(rec)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			child := log.With("worker", worker)
			for index := 0; index < 100; index++ {
				child.InfoContext(context.Background(), "tick", "index", index)
			}
		}(worker)
	}
	wg.Wait()
	if got := rec.Count(slog.LevelInfo, "tick"); got != 800 {
		t.Fatalf("Count() = %d, want 800", got)
	}
	if got := rec.Count(slog.LevelInfo, "tick", "worker", 3); got != 100 {
		t.Fatalf("Count(worker 3) = %d, want 100", got)
	}
}
