package gloq

import "log/slog"

// Preset adapts JSON output to the field names and severities a log platform
// recognizes, so records are parsed correctly without a custom pipeline.
// Presets change the built-in time, level, message, and source fields, and
// the field naming an error's type. Pretty output is not affected.
type Preset uint8

const (
	// PresetSlog writes the same field names as [slog.JSONHandler], with
	// gloq's level names. It is the default.
	PresetSlog Preset = iota
	// PresetGCP writes the fields Google Cloud Logging reads from structured
	// logs, as written by Cloud Run, GKE, and the Ops Agent: "severity" with
	// Cloud Logging severities, "message", and
	// "logging.googleapis.com/sourceLocation". SUCCESS is written as NOTICE,
	// TRACE as DEBUG, and FATAL as CRITICAL.
	PresetGCP
	// PresetDatadog writes the fields Datadog reserves: "timestamp",
	// "status", and "message", with lowercase level names that Datadog maps
	// to its statuses, such as "success" to OK. Errors name their type
	// "kind", so they fill Datadog's error.kind, error.message, and
	// error.stack attributes when logged under the key "error".
	PresetDatadog
)

// WithPreset adapts JSON output to a log platform, such as [PresetGCP].
// [WithReplaceAttr] functions still see slog's standard keys, such as
// [slog.LevelKey]; the preset renames the built-in fields after them.
func WithPreset(preset Preset) Option {
	return func(c *config) { c.preset = preset }
}

type presetSpec struct {
	timeKey, levelKey, messageKey, sourceKey string
	// errorType is written before an error's type, including its comma.
	errorType string
	// levelName names levels under levelKey. Nil keeps gloq's names.
	levelName func(level slog.Level) string
}

var presetSpecs = [...]presetSpec{
	PresetSlog: {
		timeKey:    slog.TimeKey,
		levelKey:   slog.LevelKey,
		messageKey: slog.MessageKey,
		sourceKey:  slog.SourceKey,
		errorType:  `,"type":`,
	},
	PresetGCP: {
		timeKey:    "time",
		levelKey:   "severity",
		messageKey: "message",
		sourceKey:  "logging.googleapis.com/sourceLocation",
		errorType:  `,"type":`,
		levelName:  gcpSeverity,
	},
	PresetDatadog: {
		timeKey:    "timestamp",
		levelKey:   "status",
		messageKey: "message",
		sourceKey:  slog.SourceKey,
		errorType:  `,"kind":`,
		levelName:  datadogStatus,
	},
}

func presetFor(preset Preset) *presetSpec {
	if int(preset) >= len(presetSpecs) {
		preset = PresetSlog
	}
	return &presetSpecs[preset]
}

// builtinKey renames one of slog's built-in keys. Other keys, such as a key
// a ReplaceAttr function chose, are kept.
func (p *presetSpec) builtinKey(key string) string {
	switch key {
	case slog.TimeKey:
		return p.timeKey
	case slog.LevelKey:
		return p.levelKey
	case slog.MessageKey:
		return p.messageKey
	case slog.SourceKey:
		return p.sourceKey
	}
	return key
}

// Levels between the built-in ones take the name of the level below them,
// as INFO+1 is still INFO.

func gcpSeverity(level slog.Level) string {
	switch {
	case level >= LevelFatal:
		return "CRITICAL"
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARNING"
	case level >= LevelSuccess:
		return "NOTICE"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

func datadogStatus(level slog.Level) string {
	switch {
	case level >= LevelFatal:
		return "critical"
	case level >= slog.LevelError:
		return "error"
	case level >= slog.LevelWarn:
		return "warn"
	case level >= LevelSuccess:
		return "success"
	case level >= slog.LevelInfo:
		return "info"
	case level >= slog.LevelDebug:
		return "debug"
	default:
		return "trace"
	}
}
