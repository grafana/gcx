package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type layeredMigrationFixture struct {
	system string
	user   string
	local  string
}

func newLayeredMigrationFixture(t *testing.T) layeredMigrationFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	systemRoot := filepath.Join(root, "system")
	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(home, 0o700))
	require.NoError(t, os.MkdirAll(work, 0o700))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CONFIG_DIRS", systemRoot)
	t.Setenv(ConfigFileEnvVar, "")
	t.Chdir(work)
	return layeredMigrationFixture{
		system: filepath.Join(systemRoot, StandardConfigFolder, StandardConfigFileName),
		user:   filepath.Join(home, ".config", StandardConfigFolder, StandardConfigFileName),
		local:  filepath.Join(work, LocalConfigFileName),
	}
}

func writeLayeredMigrationFixture(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

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

func TestLoadForWritePreflightsAllLegacyLayersBeforeFirstSideEffect(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	user := `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: user-token
current-context: prod
`
	local := `
contexts:
  prod:
    providers:
      slo:
        org-id: "42"
`
	writeLayeredMigrationFixture(t, fixture.user, user)
	writeLayeredMigrationFixture(t, fixture.local, local)

	_, _, err := LoadForWrite(t.Context(), "", "user")
	require.ErrorContains(t, err, "cannot safely auto-migrate layered legacy configuration")
	gotUser, readErr := os.ReadFile(fixture.user)
	require.NoError(t, readErr)
	gotLocal, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.Equal(t, user, string(gotUser))
	assert.Equal(t, local, string(gotLocal))
	_, statErr := os.Stat(fixture.user + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	_, statErr = os.Stat(fixture.local + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestLoadLayeredConsolidatesLegacyMigrationWarning(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, `
contexts:
  prod:
    grafana:
      server: https://prod.example
current-context: prod
`)
	writeLayeredMigrationFixture(t, fixture.local, `
contexts:
  staging:
    grafana:
      server: https://staging.example
`)

	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(t.Context(), &warnings)
	cfg, err := LoadLayered(ctx, "")
	require.NoError(t, err)
	require.NotNil(t, cfg.Contexts["prod"])
	require.NotNil(t, cfg.Contexts["staging"])

	output := warnings.String()
	assert.Equal(t, 1, strings.Count(output, "Warning:"), output)
	assert.Contains(t, output, "layered configuration migration is incomplete")
	assert.Contains(t, output, fixture.user)
	assert.Contains(t, output, "gcx config set --file user version 1")
	assert.Contains(t, output, "gcx config edit user")
	assert.Contains(t, output, fixture.local)
	assert.Contains(t, output, "gcx config set --file local version 1")
	assert.Contains(t, output, "gcx config edit local")
	assert.NotContains(t, output, "running with in-memory config migration")

	for _, path := range []string{fixture.user, fixture.local} {
		onDisk, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.True(t, isLegacyConfig(onDisk), path)
		_, statErr := os.Stat(path + legacyBackupSuffix)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
}

func TestLoadLayeredConsolidatesLegacyMigrationWarningThroughLogger(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, `
contexts:
  prod:
    grafana:
      server: https://prod.example
current-context: prod
`)
	writeLayeredMigrationFixture(t, fixture.local, `
contexts:
  staging:
    grafana:
      server: https://staging.example
`)

	var logs bytes.Buffer
	logger := logging.NewSLogLogger(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := logging.Context(t.Context(), logger)
	_, err := LoadLayered(ctx, "")
	require.NoError(t, err)

	output := logs.String()
	assert.Equal(t, 1, strings.Count(output, "level=WARN"), output)
	assert.Contains(t, output, "layered configuration migration is incomplete")
	assert.Contains(t, output, "gcx config set --file user version 1")
	assert.Contains(t, output, "gcx config edit user")
	assert.Contains(t, output, "gcx config set --file local version 1")
	assert.Contains(t, output, "gcx config edit local")
	assert.NotContains(t, output, "running with in-memory config migration")
}

func TestLoadLayeredConsolidatedWarningPreservesCredentialStoreBlocker(t *testing.T) {
	store := withFakeKeychain(t)
	store.getErr = credentials.ErrUnavailable
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)
	writeLayeredMigrationFixture(t, fixture.local, `
contexts:
  staging:
    grafana:
      server: https://staging.example
`)

	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(t.Context(), &warnings)
	_, err := LoadLayered(ctx, "")
	require.NoError(t, err)

	output := warnings.String()
	assert.Equal(t, 1, strings.Count(output, "Warning:"), output)
	assert.Contains(t, output, "layered configuration migration is incomplete")
	assert.Contains(t, output, "gcx config set --file user version 1")
	assert.Contains(t, output, "gcx config edit user")
	assert.Contains(t, output, "a legacy credential could not be read from the credential store")
	assert.Contains(t, output, fixture.user)
	assert.NotContains(t, output, layeredMigrationReadOnlyReason)

	for _, path := range []string{fixture.user, fixture.local} {
		onDisk, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.True(t, isLegacyConfig(onDisk), path)
		_, statErr := os.Stat(path + legacyBackupSuffix)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
}

func TestLoadLayeredMixedSchemaWarningNamesOnlyLegacySources(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, `
version: 1
stacks:
  dev:
    grafana:
      server: https://dev.example
contexts:
  dev:
    stack: dev
current-context: dev
`)
	writeLayeredMigrationFixture(t, fixture.local, `
contexts:
  prod:
    grafana:
      server: https://prod.example
`)

	userBefore, err := os.ReadFile(fixture.user)
	require.NoError(t, err)
	localBefore, err := os.ReadFile(fixture.local)
	require.NoError(t, err)
	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(t.Context(), &warnings)
	cfg, err := LoadLayered(ctx, "")
	require.NoError(t, err)
	require.NotNil(t, cfg.Contexts["dev"])
	require.NotNil(t, cfg.Contexts["prod"])

	output := warnings.String()
	assert.Equal(t, 1, strings.Count(output, "Warning:"), output)
	assert.NotContains(t, output, fixture.user)
	assert.NotContains(t, output, "gcx config set --file user version 1")
	assert.Contains(t, output, fixture.local)
	assert.Contains(t, output, "gcx config set --file local version 1")
	assert.Contains(t, output, "gcx config edit local")

	userAfter, readErr := os.ReadFile(fixture.user)
	require.NoError(t, readErr)
	assert.Equal(t, userBefore, userAfter)
	localAfter, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.Equal(t, localBefore, localAfter)
	_, statErr := os.Stat(fixture.local + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestMixedLayerPartialOverlapRequiresManualRepair(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	user := `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example
contexts:
  prod:
    stack: prod
current-context: prod
`
	local := `
contexts:
  prod:
    grafana:
      org-id: 42
`
	writeLayeredMigrationFixture(t, fixture.user, user)
	writeLayeredMigrationFixture(t, fixture.local, local)

	_, err := LoadLayered(t.Context(), "")
	var incomplete *layeredMigrationIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.False(t, incomplete.targetedMigrationAllowed)
	assert.NotContains(t, err.Error(), "gcx config set")
	assert.Contains(t, err.Error(), "manual consolidation")
	assert.Contains(t, err.Error(), "gcx config edit local")

	_, _, err = LoadForWrite(t.Context(), "", "local")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "gcx config set")
	gotUser, readErr := os.ReadFile(fixture.user)
	require.NoError(t, readErr)
	gotLocal, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.Equal(t, user, string(gotUser))
	assert.Equal(t, local, string(gotLocal))
	_, statErr := os.Stat(fixture.local + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestMixedLayerStaleBackupDoesNotAuthorizeTargetedMigration(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	user := `
version: 1
stacks:
  prod:
    grafana:
      server: https://current.example
contexts:
  prod:
    stack: prod
current-context: prod
`
	local := `
contexts:
  prod:
    grafana:
      org-id: 42
`
	staleBackup := `
contexts:
  prod:
    grafana:
      server: https://stale.example
current-context: prod
`
	writeLayeredMigrationFixture(t, fixture.user, user)
	writeLayeredMigrationFixture(t, fixture.local, local)
	require.NoError(t, os.WriteFile(fixture.user+legacyBackupSuffix, []byte(staleBackup), 0o600))

	_, err := LoadLayered(t.Context(), "")
	var incomplete *layeredMigrationIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.False(t, incomplete.targetedMigrationAllowed)
	assert.NotContains(t, err.Error(), "gcx config set")
	assert.Contains(t, err.Error(), "gcx config edit local")

	_, _, err = LoadForWrite(t.Context(), "", "local")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "gcx config set")
	gotUser, readErr := os.ReadFile(fixture.user)
	require.NoError(t, readErr)
	gotLocal, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.Equal(t, user, string(gotUser))
	assert.Equal(t, local, string(gotLocal))
	_, statErr := os.Stat(fixture.local + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestMixedNonOverlappingLayerRetainsTargetedMigration(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, `
version: 1
stacks:
  dev:
    grafana:
      server: https://dev.example
contexts:
  dev:
    stack: dev
current-context: dev
`)
	writeLayeredMigrationFixture(t, fixture.local, `
contexts:
  prod:
    grafana:
      server: https://prod.example
`)

	_, _, err := LoadForWrite(t.Context(), "", "local")
	require.NoError(t, err)
	onDisk, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.False(t, isLegacyConfig(onDisk))
}

func TestInterruptedLayeredMigrationNamesAndCompletesEveryRemainingLayer(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(t.Context(), &warnings)
	legacy := `
contexts:
  prod:
    grafana:
      server: https://prod.example
current-context: prod
`
	writeLayeredMigrationFixture(t, fixture.system, legacy)
	writeLayeredMigrationFixture(t, fixture.user, legacy)
	writeLayeredMigrationFixture(t, fixture.local, legacy)

	// The first explicit step is safe, but necessarily leaves two legacy
	// documents. The command warns (via the same step formatter asserted below)
	// and ordinary loads return a typed, actionable recovery error.
	_, _, err := LoadForWrite(ctx, "", "user")
	require.NoError(t, err)
	assert.Contains(t, warnings.String(), "layered configuration migration is incomplete")
	assert.Contains(t, warnings.String(), "gcx config set --file system version 1")
	userBytes, readErr := os.ReadFile(fixture.user)
	require.NoError(t, readErr)
	assert.False(t, isLegacyConfig(userBytes))

	systemBefore, readErr := os.ReadFile(fixture.system)
	require.NoError(t, readErr)
	localBefore, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	_, err = LoadLayered(ctx, "")
	var incomplete *layeredMigrationIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.True(t, incomplete.targetedMigrationAllowed)
	assert.Contains(t, err.Error(), fixture.system)
	assert.Contains(t, err.Error(), "gcx config set --file system version 1")
	assert.Contains(t, err.Error(), "gcx config edit system")
	assert.Contains(t, err.Error(), fixture.local)
	assert.Contains(t, err.Error(), "gcx config set --file local version 1")
	assert.Contains(t, err.Error(), "gcx config edit local")
	afterFailedLoad, readErr := os.ReadFile(fixture.system)
	require.NoError(t, readErr)
	assert.Equal(t, systemBefore, afterFailedLoad)
	afterFailedLoad, readErr = os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.Equal(t, localBefore, afterFailedLoad)

	// Targeted writes are the only exception to the incomplete-state error, so
	// every named remaining step can finish in any layer order.
	warnings.Reset()
	_, _, err = LoadForWrite(ctx, "", "system")
	require.NoError(t, err)
	assert.Contains(t, warnings.String(), "gcx config set --file local version 1")
	_, _, err = LoadForWrite(ctx, "", "local")
	require.NoError(t, err)
	loaded, err := LoadLayered(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, "https://prod.example", loaded.Contexts["prod"].Grafana.Server)
}
