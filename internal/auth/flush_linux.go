//go:build linux

package auth

import (
	"os"

	"golang.org/x/sys/unix"
)

// flushTerminalInput discards the bytes that the terminal driver still holds
// for this file. A terminal in canonical mode delivers a line only after a
// newline, so text that the user typed without one is unreachable by a read.
// TCIFLUSH removes it.
//
// It uses SyscallConn, never File.Fd. Fd puts the descriptor back into blocking
// mode, which breaks the cancellable read that Close depends on. Control hands
// out the same descriptor and leaves the poller registration alone.
func flushTerminalInput(f *os.File) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}

	var ioctlErr error
	if err := conn.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetInt(int(fd), unix.TCFLSH, unix.TCIFLUSH)
	}); err != nil {
		return err
	}
	return ioctlErr
}
