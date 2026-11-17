//go:build darwin

package credentials

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeSecurity(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "security")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) //nolint:gosec // the fake binary must be executable
	return path
}

func TestDarwinDefaultKeychainError(t *testing.T) {
	tests := []struct {
		name        string
		binary      func(t *testing.T) string
		unavailable bool
	}{
		{"keychain present", func(t *testing.T) string { t.Helper(); return fakeSecurity(t, "exit 0") }, false},
		{"no default keychain (37)", func(t *testing.T) string { t.Helper(); return fakeSecurity(t, "exit 37") }, true},
		{"missing binary", func(*testing.T) string { return "/nonexistent/security" }, true},
		{"other failure is not fatal", func(t *testing.T) string { t.Helper(); return fakeSecurity(t, "exit 1") }, false},
		{"hang is not fatal", func(t *testing.T) string { t.Helper(); return fakeSecurity(t, "exec sleep 30") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := darwinDefaultKeychainError(tt.binary(t), 500*time.Millisecond)
			if tt.unavailable {
				require.ErrorIs(t, err, ErrUnavailable)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// A HOME without a login keychain must report unavailable before any write
// can raise the blocking "Keychain Not Found" dialog. show-keychain-info is
// read-only.
func TestOpenWithoutDefaultKeychainIsUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.ErrorIs(t, headlessKeychainError(), ErrUnavailable)
	require.ErrorIs(t, Open().Set("k", "v"), ErrUnavailable)
}
