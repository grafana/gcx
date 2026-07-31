package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ambiguousCloudDestinationConfig = `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.grafana.net
  ops:
    grafana:
      server: https://ops.grafana-ops.net
cloud:
  shared:
    token: cloud-token
contexts:
  prod:
    stack: prod
    cloud: shared
  ops:
    stack: ops
    cloud: shared
current-context: prod
`

func TestAmbiguousCloudCredentialNamesExactRawEditRecovery(t *testing.T) {
	t.Run("explicit source", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(ambiguousCloudDestinationConfig), 0o600))

		_, err := LoadLayered(t.Context(), path)
		assertAmbiguousCloudRecovery(t, err, path, "gcx config edit --config "+strconv.Quote(path))
	})

	t.Run("discovered user layer", func(t *testing.T) {
		home := t.TempDir()
		workDir := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
		t.Setenv(ConfigFileEnvVar, "")
		t.Chdir(workDir)

		path := filepath.Join(home, ".config", StandardConfigFolder, StandardConfigFileName)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(ambiguousCloudDestinationConfig), 0o600))

		_, err := LoadLayered(t.Context(), "")
		assertAmbiguousCloudRecovery(t, err, path, "gcx config edit user")
	})
}

func assertAmbiguousCloudRecovery(t *testing.T, err error, path, repairCommand string) {
	t.Helper()
	require.Error(t, err)
	var detailed gcxerrors.DetailedError
	require.ErrorAs(t, err, &detailed)
	assert.Equal(t, "Cloud credential destination is ambiguous", detailed.Summary)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "https://grafana.com")
	assert.Contains(t, err.Error(), "https://grafana-ops.com")
	assert.Contains(t, err.Error(), repairCommand)
}
