package config

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// A Windows rename can briefly deny a concurrent open. Retry only sharing
// conflicts. Other read errors still reach the caller without delay.
func readConfigFile(path string) ([]byte, error) {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		data, err := os.ReadFile(path)
		if err == nil || (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION)) || time.Now().After(deadline) {
			return data, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
