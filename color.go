package gloq

import (
	"io"
	"os"
	"strings"
)

const (
	traceColor   = "\x1b[1;97m"       // white bold
	debugColor   = "\x1b[1;96m"       // cyan bold
	infoColor    = "\x1b[1;94m"       // blue bold
	successColor = "\x1b[1;92m"       // green bold
	warnColor    = "\x1b[1;93m"       // yellow bold
	errorColor   = "\x1b[1;91m"       // red bold
	fatalColor   = "\x1b[1;38;5;208m" // orange bold

	faintColor    = "\x1b[2m"  // timestamps, sources, keys, and error details
	errorKeyColor = "\x1b[91m" // keys of logged errors

	resetColor = "\x1b[0m"
)

func shouldColor(out io.Writer, mode ColorMode) bool {
	switch mode {
	case ColorAlways:
		if file, ok := out.(*os.File); ok {
			enableVirtualTerminal(file)
		}
		return true
	case ColorNever:
		return false
	}

	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if force, ok := os.LookupEnv("FORCE_COLOR"); ok && force != "" {
		if force == "0" || strings.EqualFold(force, "false") {
			return false
		}
		if file, ok := out.(*os.File); ok {
			enableVirtualTerminal(file)
		}
		return true
	}
	file, ok := out.(*os.File)
	return ok && isTerminal(file) && enableVirtualTerminal(file)
}

func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
