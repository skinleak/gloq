package gloq

import "log/slog"

type attrTransform func(groups []string, attr slog.Attr) slog.Attr

type attrPipeline struct {
	transforms []attrTransform
	errorStack bool
}

func (p attrPipeline) apply(groups []string, attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	return p.applyResolved(groups, attr)
}

func (p attrPipeline) applyResolved(groups []string, attr slog.Attr) slog.Attr {
	if attr.Equal(slog.Attr{}) {
		return slog.Attr{}
	}
	for _, transform := range p.transforms {
		attr = transform(groups, attr)
		if attr.Equal(slog.Attr{}) {
			return slog.Attr{}
		}
		attr.Value = attr.Value.Resolve()
	}
	return attr
}

func (p attrPipeline) forJSON(groups []string, attr slog.Attr) slog.Attr {
	attr = p.apply(groups, attr)
	if attr.Equal(slog.Attr{}) {
		return slog.Attr{}
	}
	// Resolution and transforms have run; only KindAny can contain a level,
	// error, or traceStack. Avoid boxing primitive values just to inspect them.
	if attr.Value.Kind() != slog.KindAny {
		return attr
	}
	value := attr.Value.Any()
	if level, ok := value.(slog.Level); ok && attr.Key == slog.LevelKey {
		// Let JSONHandler append built-in names directly instead of invoking
		// Level.MarshalJSON, which allocates a quoted byte slice. Keep arbitrary
		// levels on the existing marshaling path for slog's offset names.
		switch level {
		case LevelTrace:
			return slog.String(slog.LevelKey, "TRACE")
		case slog.LevelDebug:
			return slog.String(slog.LevelKey, "DEBUG")
		case slog.LevelInfo:
			return slog.String(slog.LevelKey, "INFO")
		case LevelSuccess:
			return slog.String(slog.LevelKey, "SUCCESS")
		case slog.LevelWarn:
			return slog.String(slog.LevelKey, "WARN")
		case slog.LevelError:
			return slog.String(slog.LevelKey, "ERROR")
		case LevelFatal:
			return slog.String(slog.LevelKey, "FATAL")
		}
	}
	if stack, ok := value.(traceStack); ok {
		return slog.String(attr.Key, string(stack))
	}
	if err, ok := value.(error); ok {
		attr.Value = slog.AnyValue(describeError(err, p.errorStack, 0))
	}
	return attr
}

func withAttrTransform(transform attrTransform) Option {
	return func(c *config) {
		if transform != nil {
			c.attrTransforms = append(c.attrTransforms, transform)
		}
	}
}
