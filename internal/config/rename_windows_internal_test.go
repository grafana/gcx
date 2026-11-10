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

func TestRenameConfigFileRetriesOpenDestination(t *testing.T) {
	dir := t.TempDir()
	oldpath := filepath.Join(dir, "config.yaml.tmp")
	newpath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(oldpath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newpath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(newpath)
	if err != nil {
		t.Fatal(err)
	}
	// Permit reads and writes but deny delete sharing, as a Windows reader can.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldpath, newpath); !errors.Is(err, windows.ERROR_ACCESS_DENIED) &&
		!errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		_ = windows.CloseHandle(handle)
		t.Fatalf("rename with open destination = %v, want transient access error", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = windows.CloseHandle(handle)
		close(released)
	}()
	defer func() { <-released }()
	if err := renameConfigFileWithRetry(oldpath, newpath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(newpath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("contents = %q, want new", got)
	}
}

func TestRenameConfigFileReturnsMissingSource(t *testing.T) {
	dir := t.TempDir()
	err := renameConfigFileWithRetry(filepath.Join(dir, "missing.tmp"), filepath.Join(dir, "config.yaml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rename error = %v, want missing file", err)
	}
}
