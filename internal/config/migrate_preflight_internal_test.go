package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreflightLayeredSourcesRejectsPartialLegacyStackOverlay(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.yaml")
	localPath := filepath.Join(t.TempDir(), LocalConfigFileName)
	user := []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: user-token
current-context: prod
`)
	local := []byte(`
contexts:
  prod:
    providers:
      slo:
        org-id: "42"
`)
	require.NoError(t, os.WriteFile(userPath, user, 0o600))
	require.NoError(t, os.WriteFile(localPath, local, 0o600))

	err := preflightLayeredSources([]ConfigSource{
		{Path: userPath, Type: "user"},
		{Path: localPath, Type: "local"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot safely auto-migrate layered legacy configuration")
	assert.Contains(t, err.Error(), `context "prod" grafana connection/auth changed`)
	gotUser, readErr := os.ReadFile(userPath)
	require.NoError(t, readErr)
	gotLocal, readErr := os.ReadFile(localPath)
	require.NoError(t, readErr)
	assert.Equal(t, user, gotUser)
	assert.Equal(t, local, gotLocal)
	_, statErr := os.Stat(userPath + legacyBackupSuffix)
	assert.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(localPath + legacyBackupSuffix)
	assert.True(t, os.IsNotExist(statErr))
}

func TestPreflightLayeredSourcesPreservesLegacyTempoMergeSemantics(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.yaml")
	localPath := filepath.Join(t.TempDir(), LocalConfigFileName)
	require.NoError(t, os.WriteFile(userPath, []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.example
    default-tempo-datasource: user-tempo
current-context: prod
`), 0o600))
	require.NoError(t, os.WriteFile(localPath, []byte(`
contexts:
  prod:
    default-tempo-datasource: local-tempo
`), 0o600))

	err := preflightLayeredSources([]ConfigSource{
		{Path: userPath, Type: "user"},
		{Path: localPath, Type: "local"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `context "prod" datasource defaults changed`)
}

func TestPreflightLayeredSourcesAllowsRepresentableLegacyOverlay(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.yaml")
	localPath := filepath.Join(t.TempDir(), LocalConfigFileName)
	require.NoError(t, os.WriteFile(userPath, []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.grafana.net
current-context: prod
`), 0o600))
	require.NoError(t, os.WriteFile(localPath, []byte(`
contexts:
  prod:
    cloud:
      token: local-token
`), 0o600))

	err := preflightLayeredSources([]ConfigSource{
		{Path: userPath, Type: "user"},
		{Path: localPath, Type: "local"},
	})
	require.NoError(t, err)
}

func TestPreflightLayeredSourcesRejectsFutureVersionBeforeMigration(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "config.yaml")
	futurePath := filepath.Join(t.TempDir(), LocalConfigFileName)
	require.NoError(t, os.WriteFile(legacyPath, []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.example
current-context: prod
`), 0o600))
	require.NoError(t, os.WriteFile(futurePath, []byte(`
version: 999
contexts:
  prod: {}
current-context: prod
`), 0o600))

	err := preflightLayeredSources([]ConfigSource{
		{Path: legacyPath, Type: "user"},
		{Path: futurePath, Type: "local"},
	})

	var versionErr UnsupportedVersionError
	require.ErrorAs(t, err, &versionErr)
	assert.Equal(t, int64(999), versionErr.Version)
	_, statErr := os.Stat(legacyPath + legacyBackupSuffix)
	assert.True(t, os.IsNotExist(statErr))
}
