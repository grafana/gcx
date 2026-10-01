// Package xdg implements the subset of the XDG Base Directory Specification
// used by gcx (config home, state home, system config dirs). It replaces the
// github.com/adrg/xdg dependency.
package xdg

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/host"
)

// ConfigHome returns the XDG config home directory.
// Reads $XDG_CONFIG_HOME at call time; defaults to $HOME/.config.
func ConfigHome(ctx context.Context) string {
	if v := host.Getenv(ctx, "XDG_CONFIG_HOME"); v != "" {
		return v
	}
	if home, err := host.UserHomeDir(ctx); err == nil {
		return filepath.Join(home, ".config")
	}
	return ""
}

// StateHome returns the XDG state home directory.
// Reads $XDG_STATE_HOME at call time; defaults to $HOME/.local/state.
func StateHome(ctx context.Context) string {
	if v := host.Getenv(ctx, "XDG_STATE_HOME"); v != "" {
		return v
	}
	if home, err := host.UserHomeDir(ctx); err == nil {
		return filepath.Join(home, ".local", "state")
	}
	return ""
}

// ConfigDirs returns the list of XDG system config directories.
// Reads $XDG_CONFIG_DIRS at call time; defaults to ["/etc/xdg"].
func ConfigDirs(ctx context.Context) []string {
	if v := host.Getenv(ctx, "XDG_CONFIG_DIRS"); v != "" {
		return strings.Split(v, string(os.PathListSeparator))
	}
	return []string{"/etc/xdg"}
}

// ConfigFile returns the full path for a config file relative to ConfigHome,
// creating intermediate directories as needed.
func ConfigFile(ctx context.Context, relPath string) (string, error) {
	p := filepath.Join(ConfigHome(ctx), relPath)
	if err := host.MkdirAll(ctx, filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, nil
}
