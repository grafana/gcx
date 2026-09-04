//go:build !darwin && !linux && !windows

package experiments

import "os"

// The caller checks that outputDir is absent before reaching this portable
// fallback. Unlike the Linux, macOS, and Windows implementations, os.Rename
// cannot prevent a concurrently created empty directory from being replaced.
func publishDirectoryNoReplace(stagingDir, outputDir string) error {
	return os.Rename(stagingDir, outputDir)
}
