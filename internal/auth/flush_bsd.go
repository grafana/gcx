//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package auth

import (
	"os"

	"golang.org/x/sys/unix"
)

// flushRead is the FREAD flag from <sys/file.h>. TIOCFLUSH takes it to mean
// "discard the input queue". x/sys/unix does not export the constant.
const flushRead = 0x1

// flushTerminalInput discards the bytes that the terminal driver still holds
// for this file. A terminal in canonical mode delivers a line only after a
// newline, so text that the user typed without one is unreachable by a read.
// TIOCFLUSH with FREAD removes it. It is the BSD equivalent of TCIFLUSH.
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
		ioctlErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCFLUSH, flushRead)
	}); err != nil {
		return err
	}
	return ioctlErr
}
