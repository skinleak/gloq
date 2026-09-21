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
	if attr.Key == slog.LevelKey && attr.Value.Any() == LevelFatal {
		return slog.String(slog.LevelKey, "FATAL")
	}
	if attr.Key == slog.LevelKey && attr.Value.Any() == LevelSuccess {
		return slog.String(slog.LevelKey, "SUCCESS")
	}
	if attr.Key == slog.LevelKey && attr.Value.Any() == LevelTrace {
		return slog.String(slog.LevelKey, "TRACE")
	}
	if stack, ok := attr.Value.Any().(traceStack); ok {
		return slog.String(attr.Key, string(stack))
	}
	if err, ok := attr.Value.Any().(error); ok {
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
