//go:build windows

package gloq

import (
	"os"
	"syscall"
)

const enableVirtualTerminalProcessing = 0x0004

var setConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// enableVirtualTerminal turns on ANSI escape code support for a Windows
// console. Consoles that cannot enable it would print raw escape codes, so
// colors stay off for them.
func enableVirtualTerminal(file *os.File) bool {
	handle := syscall.Handle(file.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	ok, _, _ := setConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	return ok != 0
}
