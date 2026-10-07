//go:build go1.22

// Go 1.21's slog differs in edge cases, such as panicking marshalers and
// empty groups, so the comparison runs on later releases only.

package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The native JSON encoder must write the same records as the original
// handler built on slog.JSONHandler. Outputs are compared as JSON token
// streams, so key order and duplicate keys count but equivalent string
// escapes, such as \b and \u0008, do not.
//
// slog writes invalid JSON when ReplaceAttr removes every attribute of a
// group. Where the reference output is not valid JSON, the native output
// only has to be valid.

func jsonTokens(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var tokens []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return tokens, nil
		}
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, fmt.Sprintf("%T:%v", token, token))
		// slog sometimes writes empty groups, such as "g":{}, which gloq
		// leaves out; ignore them on both sides.
		if n := len(tokens); n >= 3 && tokens[n-1] == "json.Delim:}" && tokens[n-2] == "json.Delim:{" && strings.HasPrefix(tokens[n-3], "string:") {
			tokens = tokens[:n-3]
		}
	}
}

// compareJSONOutput reports whether the native output matches the reference,
// and whether the reference itself was valid JSON.
func compareJSONOutput(t testing.TB, reference, native []byte) (referenceValid bool) {
	t.Helper()
	nativeLines := bytes.Count(native, []byte("\n"))
	referenceLines := bytes.Count(reference, []byte("\n"))
	if nativeLines != referenceLines {
		t.Fatalf("line count = %d, want %d\nnative:    %s\nreference: %s", nativeLines, referenceLines, native, reference)
	}
	nativeRecords := bytes.SplitAfter(native, []byte("\n"))
	referenceRecords := bytes.SplitAfter(reference, []byte("\n"))
	// Both end with a newline, which leaves an empty last element.
	nativeRecords, referenceRecords = nativeRecords[:nativeLines], referenceRecords[:referenceLines]
	referenceValid = true
	for index := range referenceRecords {
		want, wantErr := jsonTokens(referenceRecords[index])
		got, gotErr := jsonTokens(nativeRecords[index])
		if gotErr != nil || !json.Valid(nativeRecords[index]) {
			t.Fatalf("native output is not valid JSON: %v\nnative:    %s\nreference: %s", gotErr, nativeRecords[index], referenceRecords[index])
		}
		if wantErr != nil || !json.Valid(referenceRecords[index]) {
			referenceValid = false
			continue
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("output differs\nnative:    %s\nreference: %s", nativeRecords[index], referenceRecords[index])
		}
	}
	return referenceValid
}

type diffJSONMarshaler struct{ Value string }

func (m diffJSONMarshaler) MarshalJSON() ([]byte, error) { return json.Marshal("custom:" + m.Value) }

type diffTextMarshaler struct{ Value string }

func (m diffTextMarshaler) MarshalText() ([]byte, error) { return []byte("text:" + m.Value), nil }

type diffFailingMarshaler struct{}

func (diffFailingMarshaler) MarshalJSON() ([]byte, error) { return nil, errors.New("cannot marshal") }

type diffPanickingMarshaler struct{}

func (diffPanickingMarshaler) MarshalJSON() ([]byte, error) { panic("marshal panicked") }

type diffNilMarshaler struct{ value string }

func (m *diffNilMarshaler) MarshalJSON() ([]byte, error) { return json.Marshal(m.value) }

type diffMarshalerError struct{}

func (diffMarshalerError) Error() string                { return "marshaler error" }
func (diffMarshalerError) MarshalJSON() ([]byte, error) { return []byte(`"as json"`), nil }

func diffValues() map[string]slog.Value {
	plain := errors.New("plain \"quoted\" <error>\n")
	wrapped := fmt.Errorf("wrapped: %w", plain)
	var typedNil *nilError
	var nilMarshaler *diffNilMarshaler
	deep := error(errors.New("bottom"))
	for i := 0; i < maxErrorDepth+3; i++ {
		deep = fmt.Errorf("layer %d: %w", i, deep)
	}
	stamp := time.Date(2026, time.October, 7, 9, 30, 15, 123456789, time.FixedZone("CEST", 2*60*60))
	return map[string]slog.Value{
		"string":         slog.StringValue("hello"),
		"empty string":   slog.StringValue(""),
		"escapes":        slog.StringValue("quote\" backslash\\ nl\n cr\r tab\t bell\a bs\b ff\f nul\x00 del\x7f"),
		"html":           slog.StringValue("<a href=\"x\">&amp;</a>"),
		"unicode":        slog.StringValue("grüße 日本 🚀 \u2028 \u2029 \u200b \ufeff"),
		"invalid utf8":   slog.StringValue("bad \xff\xfe bytes \xc3"),
		"int":            slog.Int64Value(-42),
		"int min":        slog.Int64Value(math.MinInt64),
		"uint max":       slog.Uint64Value(math.MaxUint64),
		"float":          slog.Float64Value(123.456),
		"float zero":     slog.Float64Value(0),
		"float negzero":  slog.Float64Value(math.Copysign(0, -1)),
		"float tiny":     slog.Float64Value(1e-7),
		"float small":    slog.Float64Value(0.000001),
		"float huge":     slog.Float64Value(1e21),
		"float big":      slog.Float64Value(1e20),
		"float max":      slog.Float64Value(math.MaxFloat64),
		"float denormal": slog.Float64Value(math.SmallestNonzeroFloat64),
		"float exp":      slog.Float64Value(-1.5e-9),
		"float int":      slog.Float64Value(3),
		"float nan":      slog.Float64Value(math.NaN()),
		"float inf":      slog.Float64Value(math.Inf(1)),
		"float neginf":   slog.Float64Value(math.Inf(-1)),
		"bool":           slog.BoolValue(false),
		"duration":       slog.DurationValue(1500 * time.Millisecond),
		"time":           slog.TimeValue(stamp),
		"time utc":       slog.TimeValue(stamp.UTC()),
		"time zero":      slog.TimeValue(time.Time{}),
		"time old":       slog.TimeValue(time.Date(1200, 1, 1, 0, 0, 0, 0, time.UTC)),
		"nil":            slog.AnyValue(nil),
		"struct":         slog.AnyValue(struct{ Name, HTML string }{"n", "<b>"}),
		"map":            slog.AnyValue(map[string]any{"b": 1, "a": []int{1, 2}}),
		"slice":          slog.AnyValue([]string{"x", "y"}),
		"bytes":          slog.AnyValue([]byte("raw")),
		"json marshaler": slog.AnyValue(diffJSONMarshaler{"v"}),
		"text marshaler": slog.AnyValue(diffTextMarshaler{"v"}),
		"failing":        slog.AnyValue(diffFailingMarshaler{}),
		"panicking":      slog.AnyValue(diffPanickingMarshaler{}),
		"nil marshaler":  slog.AnyValue(nilMarshaler),
		"chan":           slog.AnyValue(make(chan int)),
		"error":          slog.AnyValue(plain),
		"wrapped":        slog.AnyValue(wrapped),
		"joined":         slog.AnyValue(errors.Join(plain, nil, wrapped)),
		"typed nil":      slog.AnyValue(typedNil),
		"panicking err":  slog.AnyValue(panickingError{}),
		"panic unwrap":   slog.AnyValue(panickingUnwrapper{}),
		"cyclic":         slog.AnyValue(&cyclicError{}),
		"deep":           slog.AnyValue(deep),
		"stack error":    slog.AnyValue(stackError{}),
		"marshaler err":  slog.AnyValue(diffMarshalerError{}),
		"level":          slog.AnyValue(LevelSuccess),
		"level custom":   slog.AnyValue(slog.LevelWarn + 1),
		"gloq level":     slog.AnyValue(Level(LevelTrace)),
		"source":         slog.AnyValue(&slog.Source{Function: "pkg.F", File: "/src/f.go", Line: 3}),
		"source partial": slog.AnyValue(&slog.Source{File: "f.go"}),
		"source empty":   slog.AnyValue(&slog.Source{}),
		"source nil":     slog.AnyValue((*slog.Source)(nil)),
		"trace stack":    slog.AnyValue(traceStack{{Function: "a.b", File: "a.go", Line: 1}, {Function: "c\"d", File: "c.go", Line: 2}}),
		"trace empty":    slog.AnyValue(traceStack{}),
		"log valuer":     slog.AnyValue(attrTestLogValuer(func() slog.Value { return slog.StringValue("resolved") })),
		"valuer error":   slog.AnyValue(attrTestLogValuer(func() slog.Value { return slog.AnyValue(plain) })),
		"valuer group":   slog.AnyValue(attrTestLogValuer(func() slog.Value { return slog.GroupValue(slog.Int("n", 1)) })),
		"recursive":      slog.AnyValue(recursiveLogValuer{}),
		"group":          slog.GroupValue(slog.String("a", "1"), slog.Group("inner", slog.Int("b", 2), slog.Any("error", plain))),
		"group empty":    slog.GroupValue(),
		"group of empty": slog.GroupValue(slog.Group("e1"), slog.Group("e2")),
		"group inline":   slog.GroupValue(slog.Group("", slog.Int("inlined", 1)), slog.Any("", nil)),
	}
}

type diffTransform struct {
	name string
	// replace is called by both handlers; calls lists what it saw.
	replace func(calls *[]string) func([]string, slog.Attr) slog.Attr
}

func recordCall(calls *[]string, groups []string, attr slog.Attr) {
	*calls = append(*calls, strings.Join(groups, ".")+"|"+attr.Key+"|"+attr.Value.Kind().String())
}

var diffTransforms = []diffTransform{
	{name: "none"},
	{name: "identity", replace: func(calls *[]string) func([]string, slog.Attr) slog.Attr {
		return func(groups []string, attr slog.Attr) slog.Attr {
			recordCall(calls, groups, attr)
			return attr
		}
	}},
	{name: "rewrite", replace: func(calls *[]string) func([]string, slog.Attr) slog.Attr {
		return func(groups []string, attr slog.Attr) slog.Attr {
			recordCall(calls, groups, attr)
			switch {
			case attr.Key == slog.TimeKey && len(groups) == 0:
				return slog.Attr{}
			case attr.Key == slog.MessageKey && len(groups) == 0:
				attr.Value = slog.StringValue("rewritten \"msg\"")
			case attr.Key == slog.LevelKey && len(groups) == 0:
				attr.Key = "severity"
			case attr.Key == slog.SourceKey && len(groups) == 0:
				if source, ok := attr.Value.Any().(*slog.Source); ok {
					source.File = "redacted.go"
				}
			case attr.Key == "line" && len(groups) == 1 && groups[0] == slog.SourceKey:
				attr.Value = slog.IntValue(1)
			case attr.Key == "function":
				return slog.Attr{}
			case attr.Key == "secret":
				attr.Value = slog.StringValue("***")
			case attr.Key == "expand":
				attr.Value = slog.GroupValue(slog.Int("x", 1), slog.Any("err", errors.New("inner")))
			case attr.Key == "as source":
				attr.Value = slog.AnyValue(&slog.Source{File: "made.go", Line: 9})
			case attr.Key == "as level":
				attr.Key = slog.LevelKey
				attr.Value = slog.AnyValue(LevelFatal)
			case attr.Key == "as valuer":
				attr.Value = slog.AnyValue(attrTestLogValuer(func() slog.Value { return slog.AnyValue(errors.New("late")) }))
			case attr.Key == "as nil":
				return slog.Attr{}
			}
			return attr
		}
	}},
}

type diffShape struct {
	name  string
	build func(slog.Handler) slog.Handler
}

var diffShapes = []diffShape{
	{"plain", func(h slog.Handler) slog.Handler { return h }},
	{"attrs", func(h slog.Handler) slog.Handler {
		return h.WithAttrs([]slog.Attr{slog.String("service", "api"), slog.Any("bound error", errors.New("bound"))})
	}},
	{"group", func(h slog.Handler) slog.Handler { return h.WithGroup("request") }},
	{"group attrs group", func(h slog.Handler) slog.Handler {
		return h.WithGroup("outer").WithAttrs([]slog.Attr{slog.Int("id", 7), slog.Any(slog.LevelKey, LevelTrace)}).WithGroup("inner \"q\"")
	}},
	{"nested attrs", func(h slog.Handler) slog.Handler {
		return h.WithAttrs([]slog.Attr{slog.Int("a", 1)}).WithGroup("g").WithAttrs([]slog.Attr{slog.Int("b", 2)}).
			WithAttrs([]slog.Attr{slog.Group("empty")}).WithAttrs([]slog.Attr{slog.Int("c", 3)}).WithGroup("h")
	}},
	{"empty group name", func(h slog.Handler) slog.Handler { return h.WithGroup("").WithAttrs(nil) }},
	{"only empty attrs", func(h slog.Handler) slog.Handler {
		return h.WithGroup("g").WithAttrs([]slog.Attr{slog.Group("e"), slog.Any("", nil)})
	}},
}

type diffConfig struct {
	name    string
	options []Option
}

var diffConfigs = []diffConfig{
	{"default", nil},
	{"no source", []Option{WithSource(false)}},
	{"utc stack", []Option{WithTimeZone(time.UTC), WithErrorStack(true)}},
	{"context", []Option{WithSource(false), WithContextAttrs(func(ctx context.Context) []slog.Attr {
		if id, ok := ctx.Value(diffContextKey{}).(string); ok {
			return []slog.Attr{slog.String("trace_id", id), slog.Group("empty")}
		}
		return nil
	})}},
}

type diffContextKey struct{}

func diffPC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	return pcs[0]
}

// diffRecords returns records covering every value, both alone and among
// other attributes.
func diffRecords() []slog.Record {
	stamp := time.Date(2026, time.October, 7, 9, 30, 15, 123456789, time.FixedZone("CEST", 2*60*60))
	pc := diffPC()
	var records []slog.Record
	add := func(level slog.Level, message string, attrs ...slog.Attr) {
		for _, withPC := range []uintptr{0, pc} {
			record := slog.NewRecord(stamp, level, message, withPC)
			record.AddAttrs(attrs...)
			records = append(records, record)
		}
	}
	add(slog.LevelInfo, "no attributes")
	for _, level := range []slog.Level{LevelTrace - 1, LevelTrace, slog.LevelDebug, slog.LevelInfo, slog.LevelInfo + 1, LevelSuccess, slog.LevelWarn, slog.LevelError, LevelFatal, LevelFatal + 1, 100} {
		add(level, "level", slog.Int("n", 1))
	}
	for name, value := range diffValues() {
		add(slog.LevelInfo, name, slog.Attr{Key: "value", Value: value})
		add(slog.LevelWarn, "mixed "+name, slog.String("before", "x"), slog.Attr{Key: "value", Value: value}, slog.Int("after", 2))
		add(slog.LevelInfo, "grouped "+name, slog.Group("g", slog.Attr{Key: "value", Value: value}))
		add(slog.LevelInfo, "level key "+name, slog.Attr{Key: slog.LevelKey, Value: value})
	}
	for _, key := range []string{"secret", "expand", "as source", "as level", "as valuer", "as nil", "", "key \"quoted\"\n", "\xff"} {
		add(slog.LevelInfo, "key "+key, slog.String(key, "v"), slog.Group("grp", slog.String(key, "w")))
	}
	add(slog.LevelInfo, "only removed", slog.String("as nil", "x"))
	add(slog.LevelInfo, "message with \"quotes\"\n and <html>")
	zero := slog.NewRecord(time.Time{}, slog.LevelInfo, "zero time", 0)
	records = append(records, zero)
	for _, year := range []int{-1, 0, 9999, 10000, 1200, 2300} {
		records = append(records, slog.NewRecord(time.Date(year, 1, 2, 3, 4, 5, 6, time.UTC), slog.LevelInfo, "year", 0))
	}
	return records
}

// slogLeaksGroups lists records with a group whose attributes are all
// removed. slog keeps passing that group to later ReplaceAttr calls, even
// when its output happens to be valid JSON.
var slogLeaksGroups = map[string]bool{
	"grouped source empty": true,
	"grouped source nil":   true,
	"key as nil":           true,
}

func TestJSONMatchesReference(t *testing.T) {
	records := diffRecords()
	ctx := context.WithValue(context.Background(), diffContextKey{}, "trace-1")
	divergent := 0
	for _, config := range diffConfigs {
		for _, transform := range diffTransforms {
			for _, shape := range diffShapes {
				t.Run(config.name+"/"+transform.name+"/"+shape.name, func(t *testing.T) {
					var nativeOut, referenceOut bytes.Buffer
					var nativeCalls, referenceCalls []string
					nativeOptions := append([]Option{WithFormat(FormatJSON), WithLevel(slog.Level(-100))}, config.options...)
					referenceOptions := append([]Option(nil), nativeOptions...)
					if transform.replace != nil {
						nativeOptions = append(nativeOptions, WithReplaceAttr(transform.replace(&nativeCalls)))
						referenceOptions = append(referenceOptions, WithReplaceAttr(transform.replace(&referenceCalls)))
					}
					native := shape.build(NewHandler(&nativeOut, nativeOptions...))
					reference := shape.build(newReferenceJSONHandler(&referenceOut, referenceOptions...))
					for _, record := range records {
						nativeOut.Reset()
						referenceOut.Reset()
						nativeCalls, referenceCalls = nativeCalls[:0], referenceCalls[:0]
						if err := native.Handle(ctx, record); err != nil {
							t.Fatal(err)
						}
						if err := reference.Handle(ctx, record); err != nil {
							t.Fatal(err)
						}
						if !compareJSONOutput(t, referenceOut.Bytes(), nativeOut.Bytes()) {
							divergent++
							continue
						}
						if slogLeaksGroups[record.Message] {
							continue
						}
						if strings.Join(nativeCalls, "\n") != strings.Join(referenceCalls, "\n") {
							t.Fatalf("record %q: ReplaceAttr calls differ\nnative:\n%s\nreference:\n%s", record.Message,
								strings.Join(nativeCalls, "\n"), strings.Join(referenceCalls, "\n"))
						}
					}
				})
			}
		}
	}
	t.Logf("%d records where slog's output was invalid JSON", divergent)
}

// TestJSONTraceMatchesReference compares TRACE records, whose stacks depend
// on the caller and so cannot use prepared records.
func TestJSONTraceMatchesReference(t *testing.T) {
	var nativeOut, referenceOut bytes.Buffer
	native := Wrap(slog.New(NewHandler(&nativeOut, WithFormat(FormatJSON), WithLevel(LevelTrace))))
	reference := Wrap(slog.New(newReferenceJSONHandler(&referenceOut, WithFormat(FormatJSON), WithLevel(LevelTrace))))
	for _, logger := range []*Logger{native, reference} {
		logger.Trace("traced", "n", 1)
		logger.WithGroup("g").Trace("grouped")
		logger.WithGroup("g").Trace("grouped with attrs", "n", 2)
	}
	// Both loggers were called from different lines, so blank out the lines.
	normalize := func(data []byte) []byte {
		var lines [][]byte
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			var record map[string]any
			if err := json.Unmarshal(line, &record); err != nil {
				t.Fatal(err)
			}
			blankLines(record)
			normalized, _ := json.Marshal(record)
			lines = append(lines, normalized)
		}
		return bytes.Join(lines, []byte("\n"))
	}
	if got, want := normalize(nativeOut.Bytes()), normalize(referenceOut.Bytes()); !bytes.Equal(got, want) {
		t.Fatalf("output differs\nnative:    %s\nreference: %s", got, want)
	}
	if !strings.Contains(nativeOut.String(), "TestJSONTraceMatchesReference") {
		t.Fatalf("stack does not include the caller: %s", nativeOut.String())
	}
}

func blankLines(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "line" || key == "time" {
				value[key] = 0
				continue
			}
			blankLines(child)
		}
	case []any:
		for _, child := range value {
			blankLines(child)
		}
	}
}

func FuzzJSONHandler(f *testing.F) {
	f.Add("message", "key", "value", "group", int64(1), 1.5, uint8(0))
	f.Add("line\nbreak", "k\"ey", "\x00\xff\u2028", "", int64(-1), math.Inf(1), uint8(7))
	f.Add("", "", "", "g", int64(0), 1e-7, uint8(255))
	f.Fuzz(func(t *testing.T, message, key, value, group string, number int64, float float64, flags uint8) {
		options := []Option{WithFormat(FormatJSON), WithSource(flags&1 != 0)}
		if flags&2 != 0 {
			options = append(options, WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
				if attr.Key == key && len(groups) > 0 {
					attr.Key = value
				}
				return attr
			}))
		}
		var nativeOut, referenceOut bytes.Buffer
		native := NewHandler(&nativeOut, options...)
		reference := newReferenceJSONHandler(&referenceOut, options...)
		if flags&4 != 0 {
			native = native.WithGroup(group)
			reference = reference.WithGroup(group)
		}
		if flags&8 != 0 {
			bound := []slog.Attr{slog.String(key, value), slog.Group(group, slog.Int64(key, number))}
			native = native.WithAttrs(bound)
			reference = reference.WithAttrs(bound)
		}
		stamp := time.Unix(number%(1<<34), number%1e9)
		if flags&16 != 0 {
			stamp = time.Time{}
		}
		record := slog.NewRecord(stamp, slog.Level(number%20), message, diffPC())
		if flags&32 != 0 {
			record.AddAttrs(
				slog.String(key, value),
				slog.Float64(value, float),
				slog.Group(group, slog.Int64(key, number), slog.Any(value, errors.New(message))),
				slog.Any(key, []string{value, message}),
			)
		}
		if err := native.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		if err := reference.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		compareJSONOutput(t, referenceOut.Bytes(), nativeOut.Bytes())
	})
}
