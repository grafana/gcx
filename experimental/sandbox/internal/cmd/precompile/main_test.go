package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The cache wazero writes owner-only becomes readable by any user.
func TestMakeReadable(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "wazero-version")
	entry := filepath.Join(sub, "entry")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("code"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := makeReadable(dir); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]fs.FileMode{dir: fs.ModeDir | 0o755, sub: fs.ModeDir | 0o755, entry: 0o644} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != want {
			t.Errorf("%s: mode %v, want %v", path, info.Mode(), want)
		}
	}
}
