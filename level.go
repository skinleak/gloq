package gloq

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const (
	// LevelTrace is for very detailed diagnostics and execution tracing.
	LevelTrace = slog.LevelDebug - 4
	// LevelSuccess is for successful or positive events.
	LevelSuccess = slog.LevelInfo + 2
	// LevelFatal is for errors that stop the program.
	LevelFatal = slog.LevelError + 4
)

// levelWidth is the length of the longest built-in level name, SUCCESS.
const levelWidth = 7

// Level is a [slog.Level] that knows gloq's level names. It implements
// [slog.Leveler], [encoding.TextMarshaler], and [encoding.TextUnmarshaler], so
// it can be read from flags, environment variables, and configuration files:
//
//	var level gloq.Level
//	flag.TextVar(&level, "level", gloq.Level(slog.LevelInfo), "minimum log level")
//	logger := gloq.New(gloq.WithLevel(level))
type Level slog.Level

// Level returns the level as a [slog.Level].
func (l Level) Level() slog.Level { return slog.Level(l) }

// String returns the level's name, such as "SUCCESS" or "INFO+1".
func (l Level) String() string { return levelName(slog.Level(l)) }

// MarshalText implements [encoding.TextMarshaler].
func (l Level) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// UnmarshalText implements [encoding.TextUnmarshaler] using [ParseLevel].
func (l *Level) UnmarshalText(text []byte) error {
	level, err := ParseLevel(string(text))
	if err != nil {
		return err
	}
	*l = Level(level)
	return nil
}

// ParseLevel parses a level name such as "trace", "SUCCESS", "warning", or
// "ERROR+2", ignoring case. Plain integers are accepted as raw slog levels.
func ParseLevel(text string) (slog.Level, error) {
	name := strings.ToUpper(strings.TrimSpace(text))
	if number, err := strconv.Atoi(name); err == nil {
		return slog.Level(number), nil
	}
	offset := 0
	if index := strings.IndexAny(name, "+-"); index > 0 {
		value, err := strconv.Atoi(name[index:])
		if err != nil {
			return 0, fmt.Errorf("gloq: invalid level %q", text)
		}
		name, offset = name[:index], value
	}
	var level slog.Level
	switch name {
	case "TRACE":
		level = LevelTrace
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "SUCCESS":
		level = LevelSuccess
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	case "FATAL":
		level = LevelFatal
	default:
		return 0, fmt.Errorf("gloq: unknown level %q", text)
	}
	return level + slog.Level(offset), nil
}

func levelName(level slog.Level) string {
	name, _ := levelNameAndColor(level)
	return name
}

func levelNameAndColor(level slog.Level) (string, string) {
	switch {
	case level == LevelFatal:
		return "FATAL", fatalColor
	case level >= slog.LevelError:
		return level.String(), errorColor
	case level >= slog.LevelWarn:
		return level.String(), warnColor
	case level == LevelSuccess:
		return "SUCCESS", successColor
	case level >= slog.LevelInfo:
		return level.String(), infoColor
	case level >= slog.LevelDebug:
		return level.String(), debugColor
	case level == LevelTrace:
		return "TRACE", traceColor
	default:
		return level.String(), traceColor
	}
}
