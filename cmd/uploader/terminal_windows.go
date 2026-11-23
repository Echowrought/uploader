package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableConsoleTerminal(out *os.File) (columns int, restore func()) {
	handle := windows.Handle(out.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil {
		return 0, func() {}
	}
	wanted := mode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	restore = func() {}
	if wanted != mode {
		if windows.SetConsoleMode(handle, wanted) != nil {
			return 0, restore
		}
		restore = func() { _ = windows.SetConsoleMode(handle, mode) }
	}
	var info windows.ConsoleScreenBufferInfo
	if windows.GetConsoleScreenBufferInfo(handle, &info) != nil {
		return 80, restore
	}
	return max(1, int(info.Window.Right)-int(info.Window.Left)+1), restore
}
