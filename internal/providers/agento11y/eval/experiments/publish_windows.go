//go:build windows

package experiments

import "golang.org/x/sys/windows"

func publishDirectoryNoReplace(stagingDir, outputDir string) error {
	from, err := windows.UTF16PtrFromString(stagingDir)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(outputDir)
	if err != nil {
		return err
	}
	return windows.MoveFile(from, to)
}
