package xdg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigHome_EnvOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	assert.Equal(t, "/custom/config", xdg.ConfigHome())
}

func TestConfigHome_DefaultsToHomeDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config"), xdg.ConfigHome())
}

func TestStateHome_EnvOverride(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	assert.Equal(t, "/custom/state", xdg.StateHome())
}

func TestStateHome_DefaultsToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".local", "state"), xdg.StateHome())
}

func TestConfigDirs_EnvOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_DIRS", "/a:/b")
	assert.Equal(t, []string{"/a", "/b"}, xdg.ConfigDirs())
}

func TestConfigDirs_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_DIRS", "")
	assert.Equal(t, []string{"/etc/xdg"}, xdg.ConfigDirs())
}

func TestConfigFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := xdg.ConfigFile("gcx/config.yaml")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "gcx", "config.yaml"), path)

	// Parent directory should have been created.
	info, err := os.Stat(filepath.Join(dir, "gcx"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
