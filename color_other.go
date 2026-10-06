//go:build !windows

package gloq

import "os"

// enableVirtualTerminal reports whether file understands ANSI escape codes.
// Terminals on platforms other than Windows always do.
func enableVirtualTerminal(*os.File) bool { return true }
