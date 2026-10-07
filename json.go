package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"reflect"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// The JSON handler encodes records itself instead of wrapping
// slog.JSONHandler. Its output is the same as slog's, with gloq's level
// names, structured errors, and TRACE stacks written directly, so no
// ReplaceAttr function has to run for every attribute. Values are encoded as
// encoding/json would encode them; only values of other types are passed to
// encoding/json.
//
// It differs from slog in two places, both cases where slog writes invalid
// JSON: a group whose attributes are all removed is left out completely, and
// a time outside the years 0 to 9999 is written as a plain string.

// jsonOptions are fixed when the handler is created and shared by every
// handler derived from it.
type jsonOptions struct {
	level        slog.Leveler
	addSource    bool
	errorStack   bool
	location     *time.Location
	pipeline     attrPipeline
	contextAttrs []func(context.Context) []slog.Attr
}

type jsonHandler struct {
	out     io.Writer
	mu      *sync.Mutex
	options *jsonOptions

	// groups holds every group started with WithGroup. The first nOpen are
	// already open in prefix; Handle opens the others only when a record has
	// attributes to put in them.
	groups []string
	nOpen  int
	// prefix holds the attributes added with WithAttrs, encoded once when
	// they are added instead of for every record.
	prefix []byte
}

// jsonState encodes one record. It is pooled, so a record normally needs no
// allocations of its own.
type jsonState struct {
	options *jsonOptions
	buf     []byte
	// sep reports whether a comma is needed before the next key.
	sep bool
	// groups are the open groups, as passed to ReplaceAttr functions.
	// Like slog, groups opened while writing the built-in attributes, such
	// as the source, are not passed on.
	groups      []string
	trackGroups bool
}

var jsonStatePool = sync.Pool{
	New: func() any { return &jsonState{buf: make([]byte, 0, 1024)} },
}

func newJSONState(options *jsonOptions) *jsonState {
	s := jsonStatePool.Get().(*jsonState)
	s.options = options
	return s
}

func (s *jsonState) free() {
	if cap(s.buf) > maxPooledLine {
		return
	}
	clear(s.groups)
	s.options, s.buf, s.sep, s.groups, s.trackGroups = nil, s.buf[:0], false, s.groups[:0], false
	jsonStatePool.Put(s)
}

func (h *jsonHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.options.level.Level()
}

func (h *jsonHandler) Handle(ctx context.Context, record slog.Record) error {
	o := h.options
	s := newJSONState(o)
	defer s.free()

	s.buf = append(s.buf, '{')
	if !record.Time.IsZero() {
		stamp := record.Time
		if o.location != nil {
			stamp = stamp.In(o.location)
		}
		s.appendBuiltinTime(stamp.Round(0))
	}
	s.appendBuiltinLevel(record.Level)
	if o.addSource {
		s.appendBuiltinSource(record.PC)
	}
	s.appendBuiltinMessage(record.Message)
	s.trackGroups = true

	if len(h.prefix) > 0 {
		s.comma()
		s.buf = append(s.buf, h.prefix...)
		s.sep = h.prefix[len(h.prefix)-1] != '{'
	}

	// Groups from WithGroup are written only when something goes in them.
	s.groups = append(s.groups, h.groups[:h.nOpen]...)
	start, sep := len(s.buf), s.sep
	for _, name := range h.groups[h.nOpen:] {
		s.openGroup(name)
	}
	written := false
	record.Attrs(func(attr slog.Attr) bool {
		written = s.appendAttr(attr) || written
		return true
	})
	if ctx != nil {
		for _, extract := range o.contextAttrs {
			for _, attr := range extract(ctx) {
				written = s.appendAttr(attr) || written
			}
		}
	}
	if record.Level <= LevelTrace {
		if stack := captureTraceStack(record.PC); len(stack) > 0 {
			written = s.appendAttr(slog.Any(traceStackKey, stack)) || written
		}
	}
	open := len(h.groups)
	if !written {
		s.buf, s.sep, open = s.buf[:start], sep, h.nOpen
	}
	for ; open > 0; open-- {
		s.buf = append(s.buf, '}')
	}
	s.buf = append(s.buf, '}', '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(s.buf)
	return err
}

func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	s := &jsonState{
		options: h.options,
		// The full slice expression makes the first append copy instead of
		// writing into storage the parent handler still uses.
		buf:         h.prefix[:len(h.prefix):len(h.prefix)],
		sep:         len(h.prefix) > 0 && h.prefix[len(h.prefix)-1] != '{',
		groups:      append([]string(nil), h.groups[:h.nOpen]...),
		trackGroups: true,
	}
	for _, name := range h.groups[h.nOpen:] {
		s.openGroup(name)
	}
	written := false
	for _, attr := range attrs {
		written = s.appendAttr(attr) || written
	}
	if !written {
		return h
	}
	clone := *h
	clone.groups = h.groups[:len(h.groups):len(h.groups)]
	clone.nOpen = len(h.groups)
	clone.prefix = s.buf
	return &clone
}

func (h *jsonHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(h.groups[:len(h.groups):len(h.groups)], name)
	return &clone
}

func (s *jsonState) comma() {
	if s.sep {
		s.buf = append(s.buf, ',')
	}
}

func (s *jsonState) appendKey(key string) {
	s.comma()
	s.buf = appendJSONString(s.buf, key)
	s.buf = append(s.buf, ':')
	s.sep = true
}

func (s *jsonState) openGroup(name string) {
	s.appendKey(name)
	s.buf = append(s.buf, '{')
	s.sep = false
	if s.trackGroups {
		s.groups = append(s.groups, name)
	}
}

func (s *jsonState) closeGroup() {
	s.buf = append(s.buf, '}')
	s.sep = true
	if s.trackGroups {
		s.groups = s.groups[:len(s.groups)-1]
	}
}

// The built-in attributes are written directly unless ReplaceAttr functions
// need to see them.

func (s *jsonState) appendBuiltinTime(stamp time.Time) {
	if len(s.options.pipeline.transforms) > 0 {
		s.appendAttr(slog.Time(slog.TimeKey, stamp))
		return
	}
	s.appendKey(slog.TimeKey)
	s.buf = appendJSONTime(s.buf, stamp)
}

func (s *jsonState) appendBuiltinLevel(level slog.Level) {
	if len(s.options.pipeline.transforms) > 0 {
		s.appendAttr(slog.Any(slog.LevelKey, level))
		return
	}
	s.appendKey(slog.LevelKey)
	s.buf = appendJSONLevel(s.buf, slog.LevelKey, level)
}

func (s *jsonState) appendBuiltinSource(pc uintptr) {
	if len(s.options.pipeline.transforms) > 0 {
		// The cached source is shared, so transforms get their own copy.
		var source slog.Source
		if pc != 0 {
			source = *sourceFor(pc)
		}
		s.appendAttr(slog.Any(slog.SourceKey, &source))
		return
	}
	if pc == 0 {
		return
	}
	s.appendSource(slog.SourceKey, sourceFor(pc))
}

func (s *jsonState) appendBuiltinMessage(message string) {
	if len(s.options.pipeline.transforms) > 0 {
		s.appendAttr(slog.String(slog.MessageKey, message))
		return
	}
	s.appendKey(slog.MessageKey)
	s.buf = appendJSONString(s.buf, message)
}

// appendAttr writes an attribute and reports whether anything was written.
func (s *jsonState) appendAttr(attr slog.Attr) bool {
	attr.Value = attr.Value.Resolve()
	// Like slog, ReplaceAttr functions see the attributes inside groups but
	// not the groups themselves.
	if attr.Value.Kind() != slog.KindGroup {
		attr = s.options.pipeline.applyResolved(s.groups, attr)
		if attr.Equal(slog.Attr{}) {
			return false
		}
	}
	value := attr.Value
	switch value.Kind() {
	case slog.KindGroup:
		return s.appendGroup(attr.Key, value.Group())
	case slog.KindAny:
		switch special := value.Any().(type) {
		case *slog.Source:
			return s.appendSource(attr.Key, special)
		case slog.Level:
			s.appendKey(attr.Key)
			s.buf = appendJSONLevel(s.buf, attr.Key, special)
			return true
		case traceStack:
			s.appendKey(attr.Key)
			s.buf = appendJSONTraceStack(s.buf, special)
			return true
		case error:
			s.appendKey(attr.Key)
			s.buf = appendJSONError(s.buf, special, s.options.errorStack, 0)
			return true
		}
	}
	s.appendKey(attr.Key)
	s.buf = appendJSONValue(s.buf, value)
	return true
}

func (s *jsonState) appendGroup(key string, attrs []slog.Attr) bool {
	if len(attrs) == 0 {
		return false
	}
	// The group may still turn out empty, for example when ReplaceAttr
	// removes all of its attributes; then it is left out completely.
	start, sep, depth := len(s.buf), s.sep, len(s.groups)
	if key != "" {
		s.openGroup(key)
	}
	written := false
	for _, attr := range attrs {
		written = s.appendAttr(attr) || written
	}
	if !written {
		s.buf, s.sep, s.groups = s.buf[:start], sep, s.groups[:depth]
		return false
	}
	if key != "" {
		s.closeGroup()
	}
	return true
}

// appendSource writes a source location as a group of its non-empty fields.
// The fields pass through ReplaceAttr like any other group's.
func (s *jsonState) appendSource(key string, source *slog.Source) bool {
	if source == nil || *source == (slog.Source{}) {
		return false
	}
	var fields [3]slog.Attr
	count := 0
	if source.Function != "" {
		fields[count] = slog.String("function", source.Function)
		count++
	}
	if source.File != "" {
		fields[count] = slog.String("file", source.File)
		count++
	}
	if source.Line != 0 {
		fields[count] = slog.Int("line", source.Line)
		count++
	}
	if len(s.options.pipeline.transforms) > 0 {
		return s.appendGroup(key, fields[:count])
	}
	if key != "" {
		s.appendKey(key)
		s.buf = append(s.buf, '{')
		s.sep = false
	}
	for _, field := range fields[:count] {
		s.appendKey(field.Key)
		s.buf = appendJSONValue(s.buf, field.Value)
	}
	if key != "" {
		s.buf = append(s.buf, '}')
		s.sep = true
	}
	return true
}

// appendJSONLevel writes gloq's names for its built-in levels under the
// level key, and slog's names everywhere else.
func appendJSONLevel(buf []byte, key string, level slog.Level) []byte {
	if key == slog.LevelKey {
		switch level {
		case LevelTrace, LevelSuccess, LevelFatal:
			return appendJSONString(buf, levelName(level))
		}
	}
	return appendJSONString(buf, level.String())
}

func appendJSONValue(buf []byte, value slog.Value) []byte {
	switch value.Kind() {
	case slog.KindString:
		return appendJSONString(buf, value.String())
	case slog.KindInt64:
		return strconv.AppendInt(buf, value.Int64(), 10)
	case slog.KindUint64:
		return strconv.AppendUint(buf, value.Uint64(), 10)
	case slog.KindFloat64:
		return appendJSONFloat(buf, value.Float64())
	case slog.KindBool:
		return strconv.AppendBool(buf, value.Bool())
	case slog.KindDuration:
		return strconv.AppendInt(buf, int64(value.Duration()), 10)
	case slog.KindTime:
		return appendJSONTime(buf, value.Time())
	case slog.KindAny:
		return appendJSONAny(buf, value.Any())
	default:
		return appendJSONString(buf, value.String())
	}
}

// appendJSONFloat writes a float as encoding/json does. NaN and infinities
// have no JSON form and are reported as slog reports them.
func appendJSONFloat(buf []byte, value float64) []byte {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		buf = append(buf, `"!ERROR:json: unsupported value: `...)
		buf = strconv.AppendFloat(buf, value, 'g', -1, 64)
		return append(buf, '"')
	}
	format := byte('f')
	if abs := math.Abs(value); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	buf = strconv.AppendFloat(buf, value, format, -1, 64)
	if format == 'e' {
		// Shorten e-09 to e-9.
		if n := len(buf); n >= 4 && buf[n-4] == 'e' && buf[n-3] == '-' && buf[n-2] == '0' {
			buf[n-2] = buf[n-1]
			buf = buf[:n-1]
		}
	}
	return buf
}

// appendJSONTime writes a time in RFC 3339 format with nanoseconds.
func appendJSONTime(buf []byte, stamp time.Time) []byte {
	buf = append(buf, '"')
	buf = stamp.AppendFormat(buf, time.RFC3339Nano)
	return append(buf, '"')
}

// jsonMarshaler encodes values of types gloq does not know, as
// slog.JSONHandler does.
type jsonMarshaler struct {
	buf     bytes.Buffer
	encoder *json.Encoder
}

var jsonMarshalerPool = sync.Pool{
	New: func() any {
		m := &jsonMarshaler{}
		m.encoder = json.NewEncoder(&m.buf)
		m.encoder.SetEscapeHTML(false)
		return m
	},
}

func appendJSONAny(buf []byte, value any) (result []byte) {
	if value == nil {
		return append(buf, "null"...)
	}
	if level, ok := value.(Level); ok {
		// Level implements encoding.TextMarshaler.
		return appendJSONString(buf, level.String())
	}
	m := jsonMarshalerPool.Get().(*jsonMarshaler)
	defer func() {
		// Like slog, report a panic in a marshaling method instead of
		// crashing; a nil pointer most likely means a method that does not
		// guard against nil.
		if recovered := recover(); recovered != nil {
			if v := reflect.ValueOf(value); v.Kind() == reflect.Pointer && v.IsNil() {
				result = appendJSONString(buf, "<nil>")
			} else {
				result = appendJSONString(buf, fmt.Sprintf("!PANIC: %v", recovered))
			}
		}
		// Keep an occasional huge value from pinning its buffer.
		if m.buf.Cap() <= maxPooledLine {
			m.buf.Reset()
			jsonMarshalerPool.Put(m)
		}
	}()
	if err := m.encoder.Encode(value); err != nil {
		return appendJSONString(buf, "!ERROR:"+err.Error())
	}
	encoded := m.buf.Bytes()
	return append(buf, encoded[:len(encoded)-1]...) // drop the newline
}

// appendJSONError writes an error as an object with its message, type, and
// causes, and its stack when requested.
func appendJSONError(buf []byte, err error, includeStack bool, depth int) []byte {
	buf = append(buf, `{"message":`...)
	buf = appendJSONString(buf, errorMessage(err))
	buf = append(buf, `,"type":`...)
	buf = appendJSONString(buf, errorType(err))
	if depth < maxErrorDepth && !isNilError(err) {
		count := 0
		appendCause := func(cause error) {
			if count == 0 {
				buf = append(buf, `,"causes":[`...)
			} else {
				buf = append(buf, ',')
			}
			count++
			buf = appendJSONError(buf, cause, includeStack, depth+1)
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, cause := range unwrapMany(joined) {
				if cause != nil {
					appendCause(cause)
				}
			}
		} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			if cause := unwrapOne(wrapped); cause != nil {
				appendCause(cause)
			}
		}
		if count > 0 {
			buf = append(buf, ']')
		}
	}
	if includeStack {
		if stack := errorStack(err); stack != "" {
			buf = append(buf, `,"stack":`...)
			buf = appendJSONString(buf, stack)
		}
	}
	return append(buf, '}')
}

// errorType returns the same name as fmt's %T without formatting.
func errorType(err error) string {
	if err == nil {
		return "<nil>"
	}
	return reflect.TypeOf(err).String()
}

func appendJSONTraceStack(buf []byte, stack traceStack) []byte {
	if stack == nil {
		return append(buf, "null"...)
	}
	buf = append(buf, '[')
	for index, frame := range stack {
		if index > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, `{"function":`...)
		buf = appendJSONString(buf, frame.Function)
		buf = append(buf, `,"file":`...)
		buf = appendJSONString(buf, frame.File)
		buf = append(buf, `,"line":`...)
		buf = strconv.AppendInt(buf, int64(frame.Line), 10)
		buf = append(buf, '}')
	}
	return append(buf, ']')
}

// appendJSONString writes a quoted JSON string, escaping it as slog does:
// control characters, quotes, backslashes, U+2028, and U+2029 are escaped and
// invalid UTF-8 is replaced. HTML characters are kept.
func appendJSONString(buf []byte, value string) []byte {
	buf = append(buf, '"')
	start := 0
	for index := 0; index < len(value); {
		char := value[index]
		if char < utf8.RuneSelf {
			if char >= ' ' && char != '"' && char != '\\' {
				index++
				continue
			}
			buf = append(buf, value[start:index]...)
			switch char {
			case '"', '\\':
				buf = append(buf, '\\', char)
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				buf = append(buf, '\\', 'u', '0', '0', hexDigits[char>>4], hexDigits[char&0xf])
			}
			index++
			start = index
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, value[start:index]...)
			buf = append(buf, `�`...)
		} else if r == ' ' || r == ' ' {
			buf = append(buf, value[start:index]...)
			buf = append(buf, '\\', 'u', '2', '0', '2', hexDigits[r&0xf])
		} else {
			index += size
			continue
		}
		index += size
		start = index
	}
	buf = append(buf, value[start:]...)
	return append(buf, '"')
}
