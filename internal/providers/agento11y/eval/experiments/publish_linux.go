//go:build linux

package experiments

import "golang.org/x/sys/unix"

func publishDirectoryNoReplace(stagingDir, outputDir string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		stagingDir,
		unix.AT_FDCWD,
		outputDir,
		unix.RENAME_NOREPLACE,
	)
}
