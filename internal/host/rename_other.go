//go:build !darwin && !linux && !windows

package host

import "os"

// Callers check that newpath is absent before reaching this portable
// fallback. Unlike the Linux, macOS, and Windows implementations, os.Rename
// cannot prevent a concurrently created empty directory from being replaced.
func renameNoReplace(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
