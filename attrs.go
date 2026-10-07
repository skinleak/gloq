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
