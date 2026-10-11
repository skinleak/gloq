// Package gloqtest records log records so tests can check what code logged.
//
// A [Recorder] is a [slog.Handler] that keeps every record it is given,
// with attributes resolved and nested in their groups:
//
//	func TestCheckout(t *testing.T) {
//		rec := gloqtest.NewRecorder()
//		service := checkout.New(rec.Logger())
//
//		service.Pay(ctx, order)
//
//		rec.AssertLogged(t, slog.LevelError, "payment failed",
//			"order_id", 42,
//			"error", checkout.ErrCardDeclined, // matched with errors.Is
//		)
//		rec.AssertNotLogged(t, gloq.LevelSuccess, "order placed")
//	}
//
// Records are matched by their exact level and message and by the attributes
// they must contain; other attributes are ignored. A failed assertion lists
// every recorded record.
package gloqtest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/skinleak/gloq"
)

// Record is a recorded log record.
type Record struct {
	Time    time.Time
	Level   slog.Level
	Message string
	// PC is the program counter of the logging call, as in [slog.Record].
	PC uintptr
	// Attrs holds the record's attributes, including those added with
	// With, nested in the groups added with WithGroup. Values are resolved
	// and empty attributes and groups are left out, as a handler would.
	Attrs []slog.Attr
}

// Value returns the value of the attribute at key. Keys of nested groups are
// joined with dots, such as "http.status".
func (r Record) Value(key string) (slog.Value, bool) {
	attrs := r.Attrs
	path := strings.Split(key, ".")
	for index, name := range path {
		attr, ok := findAttr(attrs, name)
		if !ok {
			return slog.Value{}, false
		}
		if index == len(path)-1 {
			return attr.Value, true
		}
		if attr.Value.Kind() != slog.KindGroup {
			return slog.Value{}, false
		}
		attrs = attr.Value.Group()
	}
	return slog.Value{}, false
}

// Source returns the location of the logging call, or nil if it is unknown.
func (r Record) Source() *slog.Source {
	if r.PC == 0 {
		return nil
	}
	frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
	return &slog.Source{Function: frame.Function, File: frame.File, Line: frame.Line}
}

// Map returns the record's attributes as nested maps, with each value
// converted by [slog.Value.Any].
func (r Record) Map() map[string]any {
	return attrMap(r.Attrs)
}

// String formats the record on one line for failure messages.
func (r Record) String() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s %q", gloq.Level(r.Level), r.Message)
	appendAttrs(&builder, "", r.Attrs)
	return builder.String()
}

// Recorder is a [slog.Handler] that records every record at every level. It
// is safe for concurrent use, and handlers derived from it with WithAttrs or
// WithGroup record into it as well.
type Recorder struct {
	store *store
	// steps holds the attributes and groups added with WithAttrs and
	// WithGroup, oldest first.
	steps []step
}

type store struct {
	mu      sync.Mutex
	records []Record
}

// step is either a group name or attributes added with WithAttrs.
type step struct {
	group string
	attrs []slog.Attr
}

// NewRecorder creates an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{store: &store{}}
}

// Logger returns a logger that writes to the recorder.
func (r *Recorder) Logger() *gloq.Logger {
	return gloq.Wrap(slog.New(r))
}

// Enabled reports true for every level.
func (r *Recorder) Enabled(context.Context, slog.Level) bool { return true }

// Handle records a copy of record.
func (r *Recorder) Handle(_ context.Context, record slog.Record) error {
	var attrs []slog.Attr
	record.Attrs(func(attr slog.Attr) bool {
		attrs = appendResolved(attrs, attr)
		return true
	})
	// Wrap the record's attributes in the groups and bound attributes from
	// the innermost outwards. Groups that end up empty are left out.
	for index := len(r.steps) - 1; index >= 0; index-- {
		step := r.steps[index]
		if step.attrs != nil {
			attrs = append(step.attrs[:len(step.attrs):len(step.attrs)], attrs...)
		} else if len(attrs) > 0 {
			attrs = []slog.Attr{{Key: step.group, Value: slog.GroupValue(attrs...)}}
		}
	}
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.records = append(r.store.records, Record{
		Time:    record.Time,
		Level:   record.Level,
		Message: record.Message,
		PC:      record.PC,
		Attrs:   attrs,
	})
	return nil
}

// WithAttrs returns a handler that adds attrs to every record.
func (r *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	var resolved []slog.Attr
	for _, attr := range attrs {
		resolved = appendResolved(resolved, attr)
	}
	if len(resolved) == 0 {
		return r
	}
	return r.with(step{attrs: resolved})
}

// WithGroup returns a handler that nests the attributes of every record in
// a group named name.
func (r *Recorder) WithGroup(name string) slog.Handler {
	if name == "" {
		return r
	}
	return r.with(step{group: name})
}

func (r *Recorder) with(next step) *Recorder {
	steps := append(r.steps[:len(r.steps):len(r.steps)], next)
	return &Recorder{store: r.store, steps: steps}
}

// Records returns every recorded record, oldest first.
func (r *Recorder) Records() []Record {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	return append([]Record(nil), r.store.records...)
}

// Reset removes every recorded record.
func (r *Recorder) Reset() {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.records = nil
}

// Find returns the first record with level and message that contains the
// attributes in args, given as in [slog.Logger.Log]: key-value pairs or
// [slog.Attr] values. Attributes in a [slog.Group] are matched inside that
// group. Errors match with [errors.Is], and other values when they are
// equal as by [reflect.DeepEqual] after resolving.
func (r *Recorder) Find(level slog.Level, message string, args ...any) (Record, bool) {
	want := argsToAttrs(args)
	for _, record := range r.Records() {
		if matches(record, level, message, want) {
			return record, true
		}
	}
	return Record{}, false
}

// Count returns how many records match, in the same way as [Recorder.Find].
func (r *Recorder) Count(level slog.Level, message string, args ...any) int {
	want := argsToAttrs(args)
	count := 0
	for _, record := range r.Records() {
		if matches(record, level, message, want) {
			count++
		}
	}
	return count
}

// TB is the part of [testing.TB] that assertions need.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// AssertLogged reports an error unless a record matches, in the same way as
// [Recorder.Find], and returns the first matching record.
func (r *Recorder) AssertLogged(t TB, level slog.Level, message string, args ...any) Record {
	t.Helper()
	record, ok := r.Find(level, message, args...)
	if !ok {
		t.Errorf("no record matches %s\n%s", describe(level, message, args), r.dump())
	}
	return record
}

// AssertNotLogged reports an error if any record matches, in the same way as
// [Recorder.Find].
func (r *Recorder) AssertNotLogged(t TB, level slog.Level, message string, args ...any) {
	t.Helper()
	if record, ok := r.Find(level, message, args...); ok {
		t.Errorf("unexpected record matches %s:\n\t%s", describe(level, message, args), record)
	}
}

func (r *Recorder) dump() string {
	records := r.Records()
	if len(records) == 0 {
		return "nothing was logged"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%d records were logged:", len(records))
	for _, record := range records {
		builder.WriteString("\n\t")
		builder.WriteString(record.String())
	}
	return builder.String()
}

func describe(level slog.Level, message string, args []any) string {
	return Record{Level: level, Message: message, Attrs: argsToAttrs(args)}.String()
}

func argsToAttrs(args []any) []slog.Attr {
	if len(args) == 0 {
		return nil
	}
	// slog.Record parses arguments exactly as the logging methods do.
	var record slog.Record
	record.Add(args...)
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	return attrs
}

func matches(record Record, level slog.Level, message string, want []slog.Attr) bool {
	return record.Level == level && record.Message == message && containsAttrs(record.Attrs, want)
}

// containsAttrs reports whether every attribute in want is in attrs.
func containsAttrs(attrs, want []slog.Attr) bool {
	for _, expected := range want {
		value := expected.Value.Resolve()
		if value.Kind() == slog.KindGroup && expected.Key == "" {
			if !containsAttrs(attrs, value.Group()) {
				return false
			}
			continue
		}
		actual, ok := findAttr(attrs, expected.Key)
		if !ok || !valueMatches(actual.Value, value) {
			return false
		}
	}
	return true
}

func valueMatches(actual, want slog.Value) bool {
	if want.Kind() == slog.KindGroup {
		return actual.Kind() == slog.KindGroup && containsAttrs(actual.Group(), want.Group())
	}
	if want.Kind() == slog.KindAny && actual.Kind() == slog.KindAny {
		if target, ok := want.Any().(error); ok {
			err, ok := actual.Any().(error)
			return ok && errors.Is(err, target)
		}
		return reflect.DeepEqual(actual.Any(), want.Any())
	}
	return actual.Kind() == want.Kind() && actual.Equal(want)
}

// findAttr returns the last attribute named key, which is the one most
// handlers show when a key is repeated.
func findAttr(attrs []slog.Attr, key string) (slog.Attr, bool) {
	for index := len(attrs) - 1; index >= 0; index-- {
		if attrs[index].Key == key {
			return attrs[index], true
		}
	}
	return slog.Attr{}, false
}

// appendResolved appends attr with its values resolved, following the rules
// for slog handlers: empty attributes and groups are left out, and groups
// without a key are inlined.
func appendResolved(attrs []slog.Attr, attr slog.Attr) []slog.Attr {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return attrs
	}
	if attr.Value.Kind() != slog.KindGroup {
		return append(attrs, attr)
	}
	var children []slog.Attr
	for _, child := range attr.Value.Group() {
		children = appendResolved(children, child)
	}
	if len(children) == 0 {
		return attrs
	}
	if attr.Key == "" {
		return append(attrs, children...)
	}
	return append(attrs, slog.Attr{Key: attr.Key, Value: slog.GroupValue(children...)})
}

func attrMap(attrs []slog.Attr) map[string]any {
	result := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		if attr.Value.Kind() == slog.KindGroup {
			result[attr.Key] = attrMap(attr.Value.Group())
		} else {
			result[attr.Key] = attr.Value.Any()
		}
	}
	return result
}

func appendAttrs(builder *strings.Builder, prefix string, attrs []slog.Attr) {
	for _, attr := range attrs {
		if attr.Value.Kind() == slog.KindGroup {
			appendAttrs(builder, prefix+attr.Key+".", attr.Value.Group())
			continue
		}
		fmt.Fprintf(builder, " %s%s=%v", prefix, attr.Key, formatValue(attr.Value))
	}
}

func formatValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return fmt.Sprintf("%q", value.String())
	}
	return value.String()
}
