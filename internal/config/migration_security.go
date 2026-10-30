package config

import (
	"fmt"
	"io"
	"os"
)

// readPrivateLegacyBackup checks and reads the same open file. It rejects links
// and checks the path again after the read to detect file replacement.
func readPrivateLegacyBackup(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("migration backup is not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("migration backup changed: %s", path)
	}
	if err := privateLegacyBackupFile(file, opened); err != nil {
		return nil, fmt.Errorf("migration backup has insecure permissions: %s: %w", path, err)
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return nil, fmt.Errorf("migration backup changed: %s", path)
	}
	return contents, nil
}
