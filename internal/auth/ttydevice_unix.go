//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package auth

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// terminalDevicePath returns the device path of the terminal behind stdin,
// stderr or stdout, in that order, such as /dev/ttys003 or /dev/pts/4. It is
// the fallback for a platform whose /dev/tty cannot be polled: on macOS the
// kqueue poller rejects /dev/tty, but it accepts the terminal's own device.
//
// It compares device numbers the way ttyname(3) does, because Go has no
// portable ttyname without cgo. Only terminal names are considered, never the
// /dev/stdin family, which are aliases for file descriptors.
func terminalDevicePath() (string, bool) {
	for _, fd := range []int{0, 2, 1} {
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFCHR {
			continue
		}
		if path, ok := findCharDevice(&st); ok {
			return path, true
		}
	}
	return "", false
}

// findCharDevice returns the terminal device whose device number matches want.
func findCharDevice(want *unix.Stat_t) (string, bool) {
	pts, _ := filepath.Glob("/dev/pts/*")
	entries, _ := os.ReadDir("/dev")
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, path := range terminalDeviceCandidates(names, pts) {
		var st unix.Stat_t
		if err := unix.Stat(path, &st); err == nil && st.Mode&unix.S_IFMT == unix.S_IFCHR && st.Rdev == want.Rdev {
			return path, true
		}
	}
	return "", false
}
