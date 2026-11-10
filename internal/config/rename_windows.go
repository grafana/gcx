package config

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Readers can hold a Windows file handle without FILE_SHARE_DELETE. Wait for
// those short-lived handles before replacing the config file.
func renameConfigFileWithRetry(oldpath, newpath string) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		err := os.Rename(oldpath, newpath)
		if err == nil || (!errors.Is(err, windows.ERROR_ACCESS_DENIED) &&
			!errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
			!errors.Is(err, windows.ERROR_LOCK_VIOLATION)) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
