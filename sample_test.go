package gloq

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// countingHandler counts the records it receives by message.
type countingHandler struct {
	mu     *sync.Mutex
	counts map[string]int
}

func newCountingHandler() *countingHandler {
	return &countingHandler{mu: &sync.Mutex{}, counts: map[string]int{}}
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *countingHandler) WithGroup(string) slog.Handler            { return h }

func (h *countingHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counts[record.Message]++
	return nil
}

func (h *countingHandler) count(message string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[message]
}

func handleAt(t *testing.T, handler slog.Handler, at time.Time, level slog.Level, message string) {
	t.Helper()
	if err := handler.Handle(context.Background(), slog.NewRecord(at, level, message, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestSampleFirstAndThereafter(t *testing.T) {
	inner := newCountingHandler()
	handler := Sample(inner, Sampling{Tick: time.Minute, First: 3, Thereafter: 10})
	start := time.Now()

	for index := 0; index < 33; index++ {
		handleAt(t, handler, start.Add(time.Duration(index)*time.Millisecond), slog.LevelInfo, "hot")
	}
	handleAt(t, handler, start, slog.LevelInfo, "other")

	// Records 1-3, then 13, 23, and 33.
	if got := inner.count("hot"); got != 6 {
		t.Fatalf("hot records written = %d, want 6", got)
	}
	if got := inner.count("other"); got != 1 {
		t.Fatalf("other records written = %d, want 1", got)
	}
}

func TestSampleStartsOverEachTick(t *testing.T) {
	inner := newCountingHandler()
	handler := Sample(inner, Sampling{Tick: time.Second, First: 2})
	start := time.Now()

	for second := 0; second < 3; second++ {
		for index := 0; index < 5; index++ {
			handleAt(t, handler, start.Add(time.Duration(second)*time.Second), slog.LevelInfo, "tick")
		}
	}
	if got := inner.count("tick"); got != 6 {
		t.Fatalf("records written = %d, want 6", got)
	}
}

func TestSampleRecordsBeforeCreation(t *testing.T) {
	inner := newCountingHandler()
	handler := Sample(inner, Sampling{Tick: time.Second, First: 1})
	before := time.Now().Add(-1500 * time.Millisecond)
	handleAt(t, handler, before, slog.LevelInfo, "old")
	handleAt(t, handler, before.Add(100*time.Millisecond), slog.LevelInfo, "old")
	handleAt(t, handler, time.Now(), slog.LevelInfo, "old")
	if got := inner.count("old"); got != 2 {
		t.Fatalf("records written = %d, want 2", got)
	}
}

func TestSampleCountsLevelsSeparately(t *testing.T) {
	inner := newCountingHandler()
	handler := Sample(inner, Sampling{Tick: time.Minute, First: 1})
	now := time.Now()
	handleAt(t, handler, now, slog.LevelInfo, "same")
	handleAt(t, handler, now, slog.LevelWarn, "same")
	handleAt(t, handler, now, slog.LevelWarn, "same")
	if got := inner.count("same"); got != 2 {
		t.Fatalf("records written = %d, want one per level", got)
	}
}

func TestSampleNeverDropsFatal(t *testing.T) {
	inner := newCountingHandler()
	handler := Sample(inner, Sampling{Tick: time.Minute})
	for index := 0; index < 5; index++ {
		handleAt(t, handler, time.Now(), LevelFatal, "fatal")
		handleAt(t, handler, time.Now(), slog.LevelError, "error")
	}
	if got := inner.count("fatal"); got != 5 {
		t.Fatalf("FATAL records written = %d, want 5", got)
	}
	if got := inner.count("error"); got != 0 {
		t.Fatalf("ERROR records written = %d, want 0", got)
	}
}

func TestSampleSharesCountersWithDerivedLoggers(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(Sample(NewHandler(&output, WithColor(ColorNever)), Sampling{Tick: time.Minute, First: 2}))
	logger.Info("shared")
	logger.With("child", true).Info("shared")
	logger.WithGroup("group").Info("shared")
	if got := strings.Count(output.String(), "shared"); got != 2 {
		t.Fatalf("records written = %d, want 2: %q", got, output.String())
	}
	if !strings.Contains(output.String(), "child=true") {
		t.Fatalf("attributes were lost: %q", output.String())
	}
}

func TestSampleConcurrentFirst(t *testing.T) {
	inner := newCountingHandler()
	handler := Sample(inner, Sampling{Tick: time.Hour, First: 100})
	now := time.Now()
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := 0; index < 100; index++ {
				_ = handler.Handle(context.Background(), slog.NewRecord(now, slog.LevelInfo, "busy", 0))
			}
		}()
	}
	wait.Wait()
	if got := inner.count("busy"); got != 100 {
		t.Fatalf("records written = %d, want 100", got)
	}
}

func BenchmarkSample(b *testing.B) {
	handler := Sample(NewHandler(io.Discard, WithFormat(FormatJSON)), Sampling{First: 10, Thereafter: 100})
	logger := slog.New(handler)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		logger.Info("sampled message", "index", index)
	}
}
