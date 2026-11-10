package telemetry_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceIDCreatesAndReuses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	id, persisted := telemetry.DeviceID()
	require.True(t, persisted)
	_, err := uuid.Parse(id)
	require.NoError(t, err)

	again, persisted := telemetry.DeviceID()
	assert.True(t, persisted)
	assert.Equal(t, id, again, "second call must return the persisted ID")

	info, err := os.Stat(telemetry.DeviceIDPath())
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestDeviceIDRewritesCorruptFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Dir(telemetry.DeviceIDPath()), 0o700))
	require.NoError(t, os.WriteFile(telemetry.DeviceIDPath(), []byte("not-a-uuid\n"), 0o600))

	id, persisted := telemetry.DeviceID()
	assert.True(t, persisted)
	_, err := uuid.Parse(id)
	require.NoError(t, err)

	data, err := os.ReadFile(telemetry.DeviceIDPath())
	require.NoError(t, err)
	assert.Equal(t, id+"\n", string(data), "corrupt file must be replaced with the fresh ID")
}

func TestDeviceIDEphemeralWhenStateHomeUnknown(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	assert.Empty(t, telemetry.DeviceIDPath(), "unknown state home must not yield a relative path")

	id, persisted := telemetry.DeviceID()
	assert.False(t, persisted, "unknown state home must yield an ephemeral ID")
	_, err := uuid.Parse(id)
	require.NoError(t, err)
}

func TestDeviceIDEphemeralWhenStateDirUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions do not apply to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	stateHome := t.TempDir()
	require.NoError(t, os.Chmod(stateHome, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(stateHome, 0o700)) })
	t.Setenv("XDG_STATE_HOME", stateHome)

	id, persisted := telemetry.DeviceID()
	assert.False(t, persisted, "unwritable state directory must yield an ephemeral ID")
	_, err := uuid.Parse(id)
	require.NoError(t, err)

	other, _ := telemetry.DeviceID()
	assert.NotEqual(t, id, other, "ephemeral IDs must not repeat across invocations")
}

func TestDeviceIDEphemeralWhenStateHomeIsFile(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(stateHome, []byte("file, not directory"), 0o600))
	t.Setenv("XDG_STATE_HOME", stateHome)

	id, persisted := telemetry.DeviceID()
	assert.False(t, persisted, "state home file must yield an ephemeral ID")
	_, err := uuid.Parse(id)
	require.NoError(t, err)

	other, _ := telemetry.DeviceID()
	assert.NotEqual(t, id, other, "ephemeral IDs must not repeat across invocations")
}
