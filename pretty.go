package gloq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// prettyOptions are fixed when the handler is created and shared by every
// handler derived from it.
type prettyOptions struct {
	level        slog.Leveler
	addSource    bool
	sourcePath   SourcePath
	errorStack   bool
	color        bool
	timeFormat   string
	location     *time.Location
	pipeline     attrPipeline
	contextAttrs []func(context.Context) []slog.Attr
}

type prettyHandler struct {
	out     io.Writer
	mu      *sync.Mutex
	options *prettyOptions
	groups  []string

	// Attributes added with WithAttrs are rendered once, when they are added,
	// instead of for every record. Errors and stacks bound that way are kept
	// aside because they are printed after the line.
	prefix []byte
	errors []prettyError
	stacks []traceStack
}

// lineState collects one record. It is pooled, so a record normally needs no
// allocations of its own.
type lineState struct {
	buf    []byte
	errors []prettyError
	stacks []traceStack
}

var linePool = sync.Pool{
	New: func() any { return &lineState{buf: make([]byte, 0, 1024)} },
}

// maxPooledLine keeps an occasional huge record from pinning its buffer.
const maxPooledLine = 64 << 10

func (s *lineState) free() {
	if cap(s.buf) > maxPooledLine {
		return
	}
	for index := range s.errors {
		s.errors[index] = prettyError{}
	}
	for index := range s.stacks {
		s.stacks[index] = nil
	}
	s.buf, s.errors, s.stacks = s.buf[:0], s.errors[:0], s.stacks[:0]
	linePool.Put(s)
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.options.level.Level()
}

func (h *prettyHandler) Handle(ctx context.Context, record slog.Record) error {
	o := h.options
	s := linePool.Get().(*lineState)
	defer s.free()

	if !record.Time.IsZero() {
		stamp := record.Time
		if o.location != nil {
			stamp = stamp.In(o.location)
		}
		o.appendTime(s, stamp)
	}
	o.appendLevel(s, record.Level)
	if o.addSource && record.PC != 0 {
		o.appendSource(s, record.PC)
	}
	o.appendMessage(s, record.Message)

	s.buf = append(s.buf, h.prefix...)
	s.errors = append(s.errors, h.errors...)
	s.stacks = append(s.stacks, h.stacks...)
	if ctx != nil {
		for _, extract := range o.contextAttrs {
			for _, attr := range extract(ctx) {
				o.appendAttr(s, h.groups, attr)
			}
		}
	}
	record.Attrs(func(attr slog.Attr) bool {
		o.appendAttr(s, h.groups, attr)
		return true
	})
	if record.Level <= LevelTrace {
		if stack := captureTraceStack(record.PC); len(stack) > 0 {
			s.stacks = append(s.stacks, stack)
		}
	}

	s.buf = append(s.buf, '\n')
	for _, detail := range s.errors {
		s.buf = appendPrettyError(s.buf, detail, o.errorStack, o.color)
	}
	for _, stack := range s.stacks {
		s.buf = o.appendTraceStack(s.buf, stack)
	}

	// Keep concurrent log lines from being mixed together.
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(s.buf)
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := h.clone()
	s := &lineState{buf: clone.prefix, errors: clone.errors, stacks: clone.stacks}
	for _, attr := range attrs {
		h.options.appendAttr(s, h.groups, attr)
	}
	clone.prefix, clone.errors, clone.stacks = s.buf, s.errors, s.stacks
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
	// Full slice expressions make the next append copy instead of writing
	// into storage the parent handler still uses.
	clone.groups = h.groups[:len(h.groups):len(h.groups)]
	clone.prefix = h.prefix[:len(h.prefix):len(h.prefix)]
	clone.errors = h.errors[:len(h.errors):len(h.errors)]
	clone.stacks = h.stacks[:len(h.stacks):len(h.stacks)]
	return &clone
}

func (o *prettyOptions) appendTime(s *lineState, stamp time.Time) {
	if len(o.pipeline.transforms) == 0 {
		o.faint(s)
		s.buf = stamp.AppendFormat(s.buf, o.timeFormat)
		o.reset(s)
		s.buf = append(s.buf, ' ')
		return
	}
	attr := o.pipeline.apply(nil, slog.Time(slog.TimeKey, stamp))
	if attr.Equal(slog.Attr{}) {
		return
	}
	o.faint(s)
	if attr.Value.Kind() == slog.KindTime {
		s.buf = attr.Value.Time().AppendFormat(s.buf, o.timeFormat)
	} else {
		s.buf = appendValue(s.buf, attr.Value)
	}
	o.reset(s)
	s.buf = append(s.buf, ' ')
}

func (o *prettyOptions) appendLevel(s *lineState, level slog.Level) {
	if len(o.pipeline.transforms) > 0 {
		attr := o.pipeline.apply(nil, slog.Any(slog.LevelKey, level))
		if attr.Equal(slog.Attr{}) {
			return
		}
		replaced, ok := attr.Value.Any().(slog.Level)
		if !ok {
			s.buf = appendValue(s.buf, attr.Value)
			s.buf = append(s.buf, ' ')
			return
		}
		level = replaced
	}
	name, color := levelNameAndColor(level)
	if o.color {
		s.buf = append(s.buf, color...)
	}
	s.buf = append(s.buf, name...)
	if o.color {
		s.buf = append(s.buf, resetColor...)
	}
	for padding := len(name); padding < levelWidth; padding++ {
		s.buf = append(s.buf, ' ')
	}
	s.buf = append(s.buf, ' ')
}

func (o *prettyOptions) appendSource(s *lineState, pc uintptr) {
	source := sourceFor(pc)
	if len(o.pipeline.transforms) > 0 {
		// The cached source is shared, so transforms get their own copy.
		copied := *source
		attr := o.pipeline.apply(nil, slog.Any(slog.SourceKey, &copied))
		if attr.Equal(slog.Attr{}) {
			return
		}
		replaced, ok := attr.Value.Any().(*slog.Source)
		if !ok {
			o.faint(s)
			s.buf = append(s.buf, '[')
			s.buf = appendValue(s.buf, attr.Value)
			s.buf = append(s.buf, ']')
			o.reset(s)
			s.buf = append(s.buf, ' ')
			return
		}
		source = replaced
	}
	o.faint(s)
	s.buf = append(s.buf, '[')
	s.buf = appendText(s.buf, o.sourceFile(source.File))
	s.buf = append(s.buf, ':')
	s.buf = strconv.AppendInt(s.buf, int64(source.Line), 10)
	s.buf = append(s.buf, ']')
	o.reset(s)
	s.buf = append(s.buf, ' ')
}

func (o *prettyOptions) sourceFile(file string) string {
	switch o.sourcePath {
	case SourceFull:
		return file
	case SourceDir:
		dir := filepath.Base(filepath.Dir(file))
		if dir == "." || dir == string(filepath.Separator) {
			return filepath.Base(file)
		}
		return dir + "/" + filepath.Base(file)
	default:
		return filepath.Base(file)
	}
}

func (o *prettyOptions) appendMessage(s *lineState, message string) {
	if len(o.pipeline.transforms) == 0 {
		s.buf = appendText(s.buf, message)
		return
	}
	attr := o.pipeline.apply(nil, slog.String(slog.MessageKey, message))
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindString {
		s.buf = appendText(s.buf, attr.Value.String())
	} else {
		s.buf = appendValue(s.buf, attr.Value)
	}
}

func (o *prettyOptions) appendAttr(s *lineState, groups []string, attr slog.Attr) {
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			groups = append(groups[:len(groups):len(groups)], attr.Key)
		}
		for _, child := range value.Group() {
			o.appendAttr(s, groups, child)
		}
		return
	}
	attr.Value = value
	attr = o.pipeline.applyResolved(groups, attr)
	if attr.Equal(slog.Attr{}) {
		return
	}
	value = attr.Value
	// Inspect the resolved, transformed value without boxing primitive kinds.
	if value.Kind() == slog.KindAny {
		special := value.Any()
		if stack, ok := special.(traceStack); ok {
			s.stacks = append(s.stacks, stack)
			return
		}
		if err, ok := special.(error); ok {
			s.errors = append(s.errors, prettyError{key: joinedKey(groups, attr.Key), err: err})
			return
		}
	}

	s.buf = append(s.buf, ' ')
	o.faint(s)
	s.buf = appendKey(s.buf, groups, attr.Key)
	s.buf = append(s.buf, '=')
	o.reset(s)
	s.buf = appendValue(s.buf, value)
}

func (o *prettyOptions) appendTraceStack(buf []byte, stack traceStack) []byte {
	// Color each line on its own so no escape code spills into the next record.
	open, closing := "", ""
	if o.color {
		open, closing = faintColor, resetColor
	}
	buf = append(buf, "  "...)
	buf = append(buf, open...)
	buf = append(buf, "stack:"...)
	buf = append(buf, closing...)
	buf = append(buf, '\n')
	for _, frame := range stack {
		buf = append(buf, "    "...)
		buf = append(buf, open...)
		buf = appendText(buf, frame.Function)
		buf = append(buf, closing...)
		buf = append(buf, "\n    \t"...)
		buf = append(buf, open...)
		buf = appendText(buf, frame.File)
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(frame.Line), 10)
		buf = append(buf, closing...)
		buf = append(buf, '\n')
	}
	return buf
}

func (o *prettyOptions) faint(s *lineState) {
	if o.color {
		s.buf = append(s.buf, faintColor...)
	}
}

func (o *prettyOptions) reset(s *lineState) {
	if o.color {
		s.buf = append(s.buf, resetColor...)
	}
}

// joinedKey returns the unquoted dotted key of a logged error; it is quoted
// when written.
func joinedKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	if key == "" {
		return strings.Join(groups, ".")
	}
	return strings.Join(groups, ".") + "." + key
}

// appendKey writes a dotted key and quotes it as a whole when any part
// needs quoting.
func appendKey(buf []byte, groups []string, key string) []byte {
	quote := key != "" && needsQuoting(key)
	for _, group := range groups {
		quote = quote || needsQuoting(group)
	}
	start := len(buf)
	for index, group := range groups {
		if index > 0 {
			buf = append(buf, '.')
		}
		buf = append(buf, group...)
	}
	if len(groups) > 0 && key != "" {
		buf = append(buf, '.')
	}
	buf = append(buf, key...)
	if !quote {
		return buf
	}
	joined := string(buf[start:])
	return strconv.AppendQuote(buf[:start], joined)
}

func appendValue(buf []byte, value slog.Value) []byte {
	switch value.Kind() {
	case slog.KindString:
		return appendString(buf, value.String())
	case slog.KindTime:
		return value.Time().AppendFormat(buf, time.RFC3339Nano)
	case slog.KindDuration:
		return append(buf, value.Duration().String()...)
	case slog.KindInt64:
		return strconv.AppendInt(buf, value.Int64(), 10)
	case slog.KindUint64:
		return strconv.AppendUint(buf, value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.AppendFloat(buf, value.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.AppendBool(buf, value.Bool())
	case slog.KindAny:
		if value.Any() == nil {
			return append(buf, "<nil>"...)
		}
		if err, ok := value.Any().(error); ok {
			return appendString(buf, errorMessage(err))
		}
		return appendString(buf, fmt.Sprint(value.Any()))
	default:
		return appendString(buf, value.String())
	}
}
