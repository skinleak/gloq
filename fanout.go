package gloq

import (
	"context"
	"errors"
	"log/slog"
)

// Fanout returns a handler that sends every record to each of handlers that
// is enabled for it, such as pretty output on a terminal and JSON in a file:
//
//	handler := gloq.Fanout(
//		gloq.NewHandler(os.Stderr, gloq.WithLevel(slog.LevelDebug)),
//		gloq.NewHandler(file, gloq.WithFormat(gloq.FormatJSON)),
//	)
//
// Each handler keeps its own level, format, and options. Every handler gets
// the record even if an earlier one fails, and their errors are joined. Nil
// handlers are ignored.
func Fanout(handlers ...slog.Handler) slog.Handler {
	fanout := &fanoutHandler{handlers: make([]slog.Handler, 0, len(handlers))}
	for _, handler := range handlers {
		if handler != nil {
			fanout.handlers = append(fanout.handlers, handler)
		}
	}
	return fanout
}

type fanoutHandler struct {
	handlers []slog.Handler
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		// A handler may add attributes to the record it is given, so each
		// one gets its own copy.
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := &fanoutHandler{handlers: make([]slog.Handler, len(h.handlers))}
	for index, handler := range h.handlers {
		clone.handlers[index] = handler.WithAttrs(attrs)
	}
	return clone
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := &fanoutHandler{handlers: make([]slog.Handler, len(h.handlers))}
	for index, handler := range h.handlers {
		clone.handlers[index] = handler.WithGroup(name)
	}
	return clone
}
