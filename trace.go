package gloq

import (
	"log/slog"
	"runtime"
	"sync"
)

const traceStackKey = "stack"

// traceFrame is one call in a TRACE record's stack.
type traceFrame struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// traceStack is attached to TRACE records. The handlers recognize it by type,
// so a user attribute that happens to be called "stack" is left alone.
type traceStack []traceFrame

// captureTraceStack returns the stack of the goroutine logging a TRACE record,
// starting at the record's caller. Handlers call it while the logging call is
// still on the stack, so frames inside gloq and slog are cut off by locating
// the record's own frame. A record without a usable PC gets no stack.
func captureTraceStack(pc uintptr) traceStack {
	if pc == 0 {
		return nil
	}
	caller := sourceFor(pc)
	pcs := make([]uintptr, 64)
	for {
		count := runtime.Callers(2, pcs)
		if count < len(pcs) {
			pcs = pcs[:count]
			break
		}
		pcs = make([]uintptr, len(pcs)*2)
	}

	// Usually the record's PC is on the stack as is, and the frames above it
	// need not be symbolized at all.
	for index, candidate := range pcs {
		if candidate == pc {
			return framesOf(pcs[index:])
		}
	}

	var stack traceStack
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		// Match on the resolved frame rather than the raw PC so inlined
		// callers are found as well.
		if stack == nil && frame.Function == caller.Function && frame.File == caller.File && frame.Line == caller.Line {
			stack = make(traceStack, 0, len(pcs))
		}
		if stack != nil {
			stack = append(stack, traceFrame{Function: frame.Function, File: frame.File, Line: frame.Line})
		}
		if !more {
			break
		}
	}
	if stack == nil {
		// The record was not logged by this goroutine; report its caller only.
		return traceStack{{Function: caller.Function, File: caller.File, Line: caller.Line}}
	}
	return stack
}

func framesOf(pcs []uintptr) traceStack {
	stack := make(traceStack, 0, len(pcs))
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		stack = append(stack, traceFrame{Function: frame.Function, File: frame.File, Line: frame.Line})
		if !more {
			return stack
		}
	}
}

// sourceCache maps program counters to their resolved source. Resolving a PC
// is the most expensive part of a pretty record with source enabled, and a
// program only has a fixed set of call sites.
var sourceCache sync.Map

// sourceFor returns the cached source location of pc. The result is shared
// and must not be modified.
func sourceFor(pc uintptr) *slog.Source {
	if cached, ok := sourceCache.Load(pc); ok {
		return cached.(*slog.Source)
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	source := &slog.Source{Function: frame.Function, File: frame.File, Line: frame.Line}
	cached, _ := sourceCache.LoadOrStore(pc, source)
	return cached.(*slog.Source)
}
