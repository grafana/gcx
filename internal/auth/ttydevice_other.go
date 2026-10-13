//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package auth

// terminalDevicePath has no fallback on a platform without a POSIX terminal
// driver. The terminal watchers never start there.
func terminalDevicePath() (string, bool) { return "", false }
