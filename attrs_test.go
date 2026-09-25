package gloq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

type attrTestLogValuer func() slog.Value

func (v attrTestLogValuer) LogValue() slog.Value { return v() }

type attrStringError string

func (e attrStringError) Error() string { return string(e) }

// Exercise values directly, after resolution, when bound, and after a
// transform changes their kind. In particular, a LogValuer's original kind
// must not determine whether its resolved error or stack gets special handling.
func TestAttributeKinds(t *testing.T) {
	stamp := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	err := errors.New("boom")
	var typedNil *nilError
	tests := []struct {
		name   string
		value  slog.Value
		pretty string
		json   string
	}{
		{"string", slog.StringValue("a b"), ` value="a b"` + "\n", `"a b"`},
		{"int", slog.Int64Value(-300), " value=-300\n", `-300`},
		{"uint", slog.Uint64Value(600), " value=600\n", `600`},
		{"float", slog.Float64Value(0.875), " value=0.875\n", `0.875`},
		{"bool", slog.BoolValue(true), " value=true\n", `true`},
		{"duration", slog.DurationValue(1500 * time.Millisecond), " value=1.5s\n", `1500000000`},
		{"time", slog.TimeValue(stamp), " value=2026-09-25T12:00:00Z\n", `"2026-09-25T12:00:00Z"`},
		{"any", slog.AnyValue(struct{ N int }{7}), " value={7}\n", `{"N":7}`},
		{"nil", slog.AnyValue(nil), " value=<nil>\n", `null`},
		{"error", slog.AnyValue(err), "\n  value: boom\n", `{"message":"boom","type":"*errors.errorString"}`},
		{"named string error", slog.AnyValue(attrStringError("boom")), "\n  value: boom\n", `{"message":"boom","type":"gloq.attrStringError"}`},
		{"typed nil", slog.AnyValue(typedNil), "\n  value: <nil>\n", `{"message":"<nil>","type":"*gloq.nilError"}`},
		{"trace", slog.AnyValue(traceStack("app.run\n\tapp.go:7")), "\n  stack:\n    app.run\n    \tapp.go:7\n", `"app.run\n\tapp.go:7"`},
		{"group", slog.GroupValue(slog.Group("nested", slog.Int("count", 300), slog.Any("error", err))),
			" value.nested.count=300\n  value.nested.error: boom\n",
			`{"nested":{"count":300,"error":{"message":"boom","type":"*errors.errorString"}}}`},
	}
	for _, format := range []Format{FormatPretty, FormatJSON} {
		name := "pretty"
		if format == FormatJSON {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					for _, mode := range []string{"direct", "resolved", "bound", "transformed"} {
						// Existing pretty group traversal happens before transforms;
						// do not impose different semantics for transform-created groups.
						if mode == "transformed" && test.value.Kind() == slog.KindGroup {
							continue
						}
						t.Run(mode, func(t *testing.T) {
							var output bytes.Buffer
							calls := 0
							resolve := func(value slog.Value) slog.Value {
								return slog.AnyValue(attrTestLogValuer(func() slog.Value {
									calls++
									return value
								}))
							}
							attr := slog.Attr{Key: "value", Value: test.value}
							options := []Option{WithFormat(format), WithSource(false), WithColor(ColorNever)}
							wantCalls := 0
							if mode == "resolved" || mode == "bound" {
								attr.Value = resolve(attr.Value)
								wantCalls = 1
							}
							transforms := 0
							if mode == "transformed" {
								attr.Value = resolve(slog.StringValue("input"))
								wantCalls = 2
								options = append(options, withAttrTransform(func(_ []string, a slog.Attr) slog.Attr {
									if a.Key == "value" {
										transforms++
										if a.Value.Kind() != slog.KindString || a.Value.String() != "input" {
											t.Fatalf("transform received unresolved value: %v", a)
										}
										a.Value = resolve(test.value)
									}
									return a
								}))
							}
							handler := NewHandler(&output, options...)
							record := slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0)
							if mode == "bound" {
								handler = handler.WithAttrs([]slog.Attr{attr})
							} else {
								record.AddAttrs(attr)
							}
							if err := handler.Handle(context.Background(), record); err != nil {
								t.Fatal(err)
							}
							if calls != wantCalls || (mode == "transformed" && transforms != 1) {
								t.Fatalf("LogValue calls = %d, want %d; transforms = %d", calls, wantCalls, transforms)
							}
							if format == FormatPretty {
								if got, want := output.String(), "INFO  message"+test.pretty; got != want {
									t.Fatalf("output = %q, want %q", got, want)
								}
							} else {
								var got, want any
								if err := json.Unmarshal(output.Bytes(), &got); err != nil {
									t.Fatal(err)
								}
								if err := json.Unmarshal([]byte(`{"level":"INFO","msg":"message","value":`+test.json+`}`), &want); err != nil {
									t.Fatal(err)
								}
								if !reflect.DeepEqual(got, want) || !strings.HasSuffix(output.String(), "\n") {
									t.Fatalf("output = %s, want %#v", output.String(), want)
								}
							}
						})
					}
				})
			}
		})
	}
}

func TestJSONLevelAttrKinds(t *testing.T) {
	for _, test := range []struct {
		value slog.Value
		want  slog.Value
	}{
		{slog.Int64Value(2), slog.Int64Value(2)},
		{slog.StringValue("custom"), slog.StringValue("custom")},
		{slog.AnyValue(LevelSuccess), slog.StringValue("SUCCESS")},
		{slog.AnyValue(attrTestLogValuer(func() slog.Value { return slog.AnyValue(LevelTrace) })), slog.StringValue("TRACE")},
	} {
		got := (attrPipeline{}).forJSON(nil, slog.Attr{Key: slog.LevelKey, Value: test.value})
		if !got.Value.Equal(test.want) || got.Key != slog.LevelKey {
			t.Errorf("forJSON(%v) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestGroupedLogValuerErrors(t *testing.T) {
	for _, format := range []Format{FormatPretty, FormatJSON} {
		var output bytes.Buffer
		handler := NewHandler(&output, WithFormat(format), WithSource(false), WithColor(ColorNever))
		handler = handler.WithGroup("outer").WithAttrs([]slog.Attr{
			slog.Any("bound", attrTestLogValuer(func() slog.Value { return slog.AnyValue(errors.New("bound error")) })),
		}).WithGroup("inner")
		record := slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0)
		record.AddAttrs(slog.Any("resolved", attrTestLogValuer(func() slog.Value {
			return slog.GroupValue(slog.Any("recursive", recursiveLogValuer{}))
		})))
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		if format == FormatPretty {
			for _, want := range []string{"outer.bound: bound error", "outer.inner.resolved.recursive: LogValue called too many times"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q in %q", want, output.String())
				}
			}
		} else {
			var record struct {
				Outer struct {
					Bound structuredError
					Inner struct {
						Resolved struct{ Recursive structuredError }
					}
				}
			}
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Outer.Bound.Message != "bound error" ||
				!strings.Contains(record.Outer.Inner.Resolved.Recursive.Message, "LogValue called too many times") {
				t.Fatalf("unexpected error structure: %s", output.String())
			}
		}
	}
}
