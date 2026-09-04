//go:build !darwin && !linux && !windows

package experiments

import "os"

// os.Rename rejects an existing directory on Go's supported platforms. Linux,
// macOS, and Windows use stronger kernel-level no-replace operations above.
func publishDirectoryNoReplace(stagingDir, outputDir string) error {
	return os.Rename(stagingDir, outputDir)
}
