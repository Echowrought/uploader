package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func enableConsoleTerminal(out *os.File) (columns int, restore func()) {
	fd := int(out.Fd())
	if _, err := unix.IoctlGetTermios(fd, unix.TCGETS); err != nil {
		return 0, func() {}
	}
	size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 {
		return 80, func() {}
	}
	return int(size.Col), func() {}
}
