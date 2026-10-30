//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestReadConfigFileRetriesWindowsSharingViolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	want := []byte("version: 1\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the file without share access. A concurrent open must get a
	// sharing violation until the handle closes.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		_ = windows.CloseHandle(handle)
		t.Fatalf("locked read error = %v, want sharing violation", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = windows.CloseHandle(handle)
		close(released)
	}()
	defer func() { <-released }()
	got, err := readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("contents = %q, want %q", got, want)
	}
}

func TestReadConfigFileReturnsUnrelatedError(t *testing.T) {
	_, err := readConfigFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read error = %v, want file not found", err)
	}
}
