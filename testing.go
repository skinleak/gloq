package gloq

import (
	"io"
	"log/slog"
	"strings"
)

// TestingT is the part of [testing.TB] that NewTestLogger needs.
type TestingT interface {
	Log(args ...any)
}

// NewTestLogger creates a logger that writes to a test's log, so output only
// appears for failing tests or with go test -v. It logs every level, without
// colors, unless options say otherwise.
//
//	func TestServer(t *testing.T) {
//		server := NewServer(gloq.NewTestLogger(t))
//		...
//	}
//
// Records written after the test has finished are dropped.
func NewTestLogger(t TestingT, options ...Option) *Logger {
	if t == nil {
		panic("gloq: nil test")
	}
	options = append([]Option{WithColor(ColorNever), WithLevel(LevelTrace)}, options...)
	return &Logger{slog.New(NewHandler(testWriter{t}, options...))}
}

type testWriter struct {
	t TestingT
}

func (w testWriter) Write(line []byte) (written int, err error) {
	// The testing package panics when a test logs after it has completed,
	// which background goroutines can easily do.
	defer func() {
		if recover() != nil {
			written, err = len(line), nil
		}
	}()
	// Go 1.25 added an output writer that does not prefix every line with
	// the file and line of the call to Log, which would point into gloq.
	if output, ok := w.t.(interface{ Output() io.Writer }); ok {
		return output.Output().Write(line)
	}
	w.t.Log(strings.TrimSuffix(string(line), "\n"))
	return len(line), nil
}
