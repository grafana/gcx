//go:build darwin

package experiments

import "golang.org/x/sys/unix"

func publishDirectoryNoReplace(stagingDir, outputDir string) error {
	return unix.RenameatxNp(
		unix.AT_FDCWD,
		stagingDir,
		unix.AT_FDCWD,
		outputDir,
		unix.RENAME_EXCL,
	)
}
