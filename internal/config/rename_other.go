//go:build !windows

package config

import "os"

func renameConfigFileWithRetry(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
