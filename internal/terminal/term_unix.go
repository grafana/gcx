//go:build !windows

package terminal

import (
	"syscall"
	"unsafe"
)

// isTerminal reports whether fd is a terminal.
func isTerminal(fd int) bool {
	var t syscall.Termios
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), ioctlGetTermios, uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	return err == 0
}

// getSize returns the dimensions of the terminal connected to fd.
func getSize(fd int) (int, int, error) {
	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)), 0, 0, 0)
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.Col), int(ws.Row), nil
}
