package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_explicitFile(t *testing.T) {
	req := require.New(t)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile("./testdata/config.yaml"))
	req.NoError(err)

	req.Equal("local", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
	req.Equal("local", cfg.Contexts["local"].Name)
	req.Equal("http://localhost:3000/", cfg.Contexts["local"].Grafana.Server)
}

func TestLoad_explicitFile_notFound(t *testing.T) {
	req := require.New(t)

	_, err := config.Load(t.Context(), config.ExplicitConfigFile("./testdata/does-not-exist.yaml"))
	req.Error(err)
	req.ErrorIs(err, os.ErrNotExist)
}

func TestLoad_standardLocation_noExistingConfig(t *testing.T) {
	req := require.New(t)

	fakeConfigDir := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", fakeConfigDir)

	// make sure the xdg library uses the new-fake env var we just set
	xdg.Reload()

	cfg, err := config.Load(t.Context(), config.StandardLocation())
	req.NoError(err)

	// An empty configuration is returned
	req.Equal("default", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
}

func TestLoad_standardLocation_withExistingConfig(t *testing.T) {
	req := require.New(t)

	fakeConfigDir := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", fakeConfigDir)

	// create a barebones config file at the standard location
	err := os.MkdirAll(filepath.Join(fakeConfigDir, config.StandardConfigFolder), 0777)
	req.NoError(err)

	err = os.WriteFile(
		filepath.Join(fakeConfigDir, config.StandardConfigFolder, config.StandardConfigFileName),
		[]byte(`current-context: local`),
		0600,
	)
	req.NoError(err)

	// make sure the xdg library uses the new-fake env var we just set
	xdg.Reload()

	cfg, err := config.Load(t.Context(), config.StandardLocation())
	req.NoError(err)

	req.Equal("local", cfg.CurrentContext)
	req.Empty(cfg.Contexts)
}

func TestLoad_standardLocation_withEnvVar(t *testing.T) {
	req := require.New(t)

	// Set the environment variable to point to a test config
	t.Setenv(config.ConfigFileEnvVar, "./testdata/config.yaml")

	cfg, err := config.Load(t.Context(), config.StandardLocation())
	req.NoError(err)

	req.Equal("local", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
	req.Equal("local", cfg.Contexts["local"].Name)
	req.Equal("http://localhost:3000/", cfg.Contexts["local"].Grafana.Server)
}

func TestLoad_standardLocation_envVarTakesPrecedence(t *testing.T) {
	req := require.New(t)

	fakeConfigDir := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", fakeConfigDir)

	// create a config file at the standard location with different content
	err := os.MkdirAll(filepath.Join(fakeConfigDir, config.StandardConfigFolder), 0777)
	req.NoError(err)

	err = os.WriteFile(
		filepath.Join(fakeConfigDir, config.StandardConfigFolder, config.StandardConfigFileName),
		[]byte(`current-context: standard-location`),
		0600,
	)
	req.NoError(err)

	// Set the environment variable to point to a different config
	t.Setenv(config.ConfigFileEnvVar, "./testdata/config.yaml")

	// make sure the xdg library uses the new-fake env var we just set
	xdg.Reload()

	cfg, err := config.Load(t.Context(), config.StandardLocation())
	req.NoError(err)

	// Should load from env var, not standard location
	req.Equal("local", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
	req.Equal("http://localhost:3000/", cfg.Contexts["local"].Grafana.Server)
}

func TestLoad_withOverride(t *testing.T) {
	req := require.New(t)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile("./testdata/config.yaml"), func(cfg *config.Config) error {
		cfg.CurrentContext = "overridden"
		return nil
	})
	req.NoError(err)

	req.Equal("overridden", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
	req.Equal("http://localhost:3000/", cfg.Contexts["local"].Grafana.Server)
}

func TestLoad_withInvalidYaml(t *testing.T) {
	req := require.New(t)

	cfg := `current-context: local
this-field-is-invalid: []`

	configFile := testutils.CreateTempFile(t, cfg)

	_, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	req.Error(err)
	req.ErrorAs(err, &config.UnmarshalError{})
	req.ErrorContains(err, "unknown field \"this-field-is-invalid\"")
}

func TestLoad_withProviders(t *testing.T) {
	req := require.New(t)

	configYAML := `contexts:
  default:
    grafana:
      server: http://localhost:3000/
      token: local_token
    providers:
      slo:
        token: slo-token
        url: https://slo.example.com
      oncall:
        token: oncall-token
current-context: default
`
	configFile := testutils.CreateTempFile(t, configYAML)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	req.NoError(err)

	req.Equal("default", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
	req.NotNil(cfg.Contexts["default"].Providers)
	req.Equal("slo-token", cfg.Contexts["default"].Providers["slo"]["token"])
	req.Equal("https://slo.example.com", cfg.Contexts["default"].Providers["slo"]["url"])
	req.Equal("oncall-token", cfg.Contexts["default"].Providers["oncall"]["token"])

	// Round-trip: write and reload
	tmpDir := t.TempDir()
	roundTripFile := filepath.Join(tmpDir, "config-roundtrip.yaml")
	err = config.Write(t.Context(), config.ExplicitConfigFile(roundTripFile), cfg)
	req.NoError(err)

	cfg2, err := config.Load(t.Context(), config.ExplicitConfigFile(roundTripFile))
	req.NoError(err)

	// Compare relevant fields (Source will differ)
	req.Equal(cfg.CurrentContext, cfg2.CurrentContext)
	req.Equal(cfg.Contexts["default"].Providers, cfg2.Contexts["default"].Providers)
	req.Equal(cfg.Contexts["default"].Grafana.Server, cfg2.Contexts["default"].Grafana.Server)
}

func TestWrite(t *testing.T) {
	req := require.New(t)

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	cfg := config.Config{
		CurrentContext: "local",
	}

	err := config.Write(t.Context(), config.ExplicitConfigFile(configFile), cfg)
	req.NoError(err)

	req.FileExists(configFile)
}

func TestDiscoverSources(t *testing.T) {
	systemDir := t.TempDir()
	userDir := t.TempDir()
	localDir := t.TempDir()

	// Write config files.
	systemFile := filepath.Join(systemDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(systemFile), 0o755))
	require.NoError(t, os.WriteFile(systemFile, []byte("contexts:\n  sys: {}\ncurrent-context: sys\n"), 0o600))

	userFile := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  usr: {}\ncurrent-context: usr\n"), 0o600))

	localFile := filepath.Join(localDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(localFile, []byte("contexts:\n  lcl: {}\n"), 0o600))

	sources, err := config.DiscoverSources(
		config.WithSystemDir(systemDir),
		config.WithUserDir(userDir),
		config.WithWorkDir(localDir),
	)
	require.NoError(t, err)

	require.Len(t, sources, 3)
	assert.Equal(t, "system", sources[0].Type)
	assert.Equal(t, "user", sources[1].Type)
	assert.Equal(t, "local", sources[2].Type)
	assert.Equal(t, systemFile, sources[0].Path)
	assert.Equal(t, userFile, sources[1].Path)
	assert.Equal(t, localFile, sources[2].Path)
}

func TestDiscoverSources_SkipsMissing(t *testing.T) {
	userDir := t.TempDir()
	userFile := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  usr: {}\ncurrent-context: usr\n"), 0o600))

	sources, err := config.DiscoverSources(
		config.WithSystemDir(t.TempDir()), // empty, no config
		config.WithUserDir(userDir),
		config.WithWorkDir(t.TempDir()), // empty, no .gcx.yaml
	)
	require.NoError(t, err)

	require.Len(t, sources, 1)
	assert.Equal(t, "user", sources[0].Type)
}
