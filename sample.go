package gloq

import (
	"context"
	"log/slog"
	"math"
	"sync/atomic"
	"time"
)

// Sampling limits how often records with the same level and message are
// written, so a hot loop or a failing dependency cannot flood the logs.
//
// Within every Tick, the first First records with a given level and message
// are written. After that only every Thereafter-th one is, or none at all
// when Thereafter is zero. Counting starts over with each Tick; ticks are
// counted from when the sampling handler was created.
type Sampling struct {
	// Tick is the interval after which counting starts over. Zero means one
	// second.
	Tick time.Duration
	// First is how many records per Tick are always written.
	First int
	// Thereafter writes every Thereafter-th record after the first First.
	// Zero drops all of them.
	Thereafter int
}

// sampleBuckets is the number of counters shared by all levels and messages.
// Distinct messages rarely share a counter, and when they do they are only
// sampled a little sooner.
const sampleBuckets = 4096

// Sample returns a handler that passes records to handler according to
// sampling. FATAL records are never dropped. Records are counted by level
// and message only, so the same message with different attributes counts
// as one. Loggers derived with With or WithGroup share their counters.
//
//	handler := gloq.Sample(gloq.NewHandler(os.Stderr), gloq.Sampling{
//		Tick:       time.Second,
//		First:      10,
//		Thereafter: 100,
//	})
func Sample(handler slog.Handler, sampling Sampling) slog.Handler {
	if handler == nil {
		panic("gloq: nil handler")
	}
	if sampling.Tick <= 0 {
		sampling.Tick = time.Second
	}
	if sampling.First < 0 {
		sampling.First = 0
	}
	if sampling.Thereafter < 0 {
		sampling.Thereafter = 0
	}
	return &sampleHandler{
		inner:      handler,
		tick:       int64(sampling.Tick),
		first:      uint64(sampling.First),
		thereafter: uint64(sampling.Thereafter),
		epoch:      time.Now().UnixNano(),
		counters:   new([sampleBuckets]atomic.Uint64),
	}
}

type sampleHandler struct {
	inner      slog.Handler
	tick       int64
	first      uint64
	thereafter uint64
	epoch      int64
	// Each counter holds the number of its current tick in the upper 32 bits
	// and the records counted in that tick in the lower 32, so both change
	// together.
	counters *[sampleBuckets]atomic.Uint64
}

// sampleCount counts one record in tick and returns its number in that tick.
func sampleCount(counter *atomic.Uint64, tick uint32) uint64 {
	for {
		old := counter.Load()
		next := uint64(tick)<<32 | 1
		if uint32(old>>32) == tick {
			if uint32(old) == math.MaxUint32 {
				return math.MaxUint32
			}
			next = old + 1
		}
		if counter.CompareAndSwap(old, next) {
			return next & math.MaxUint32
		}
	}
}

func (h *sampleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *sampleHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= LevelFatal {
		return h.inner.Handle(ctx, record)
	}
	now := record.Time
	if now.IsZero() {
		now = time.Now()
	}
	elapsed := now.UnixNano() - h.epoch
	tick := elapsed / h.tick
	if elapsed < 0 && elapsed%h.tick != 0 {
		tick-- // Round down for records timed before the handler was created.
	}
	counter := &h.counters[sampleKey(record.Level, record.Message)%sampleBuckets]
	count := sampleCount(counter, uint32(tick))
	if count <= h.first || (h.thereafter > 0 && (count-h.first)%h.thereafter == 0) {
		return h.inner.Handle(ctx, record)
	}
	return nil
}

func (h *sampleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.inner = h.inner.WithAttrs(attrs)
	return &clone
}

func (h *sampleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.inner = h.inner.WithGroup(name)
	return &clone
}

// sampleKey is the FNV-1a hash of level and message.
func sampleKey(level slog.Level, message string) uint64 {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	hash := uint64(offset)
	hash = (hash ^ uint64(int64(level))) * prime
	for index := 0; index < len(message); index++ {
		hash = (hash ^ uint64(message[index])) * prime
	}
	return hash
}
