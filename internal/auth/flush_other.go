//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package auth

import "os"

// flushTerminalInput does nothing on a platform without a POSIX terminal
// driver. The paste watcher never starts there, because openPasteTerminal
// cannot open /dev/tty.
func flushTerminalInput(_ *os.File) error { return nil }
