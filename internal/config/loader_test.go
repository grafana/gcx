package config_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keychainPolicyFixture makes source discovery deterministic while exercising
// the real layered loader. The credential backend itself is always the test
// fake installed by withFakeStore, so these policy tests never open the OS
// keychain.
type keychainPolicyFixture struct {
	system   string
	user     string
	local    string
	explicit string
}

func newKeychainPolicyFixture(t *testing.T) keychainPolicyFixture {
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
	t.Setenv(config.ConfigFileEnvVar, "")
	t.Setenv("GCX_KEYCHAIN", "")
	t.Chdir(work)

	return keychainPolicyFixture{
		system:   filepath.Join(systemRoot, config.StandardConfigFolder, config.StandardConfigFileName),
		user:     filepath.Join(home, ".config", config.StandardConfigFolder, config.StandardConfigFileName),
		local:    filepath.Join(work, config.LocalConfigFileName),
		explicit: filepath.Join(root, "explicit.yaml"),
	}
}

func writeKeychainPolicyConfig(t *testing.T, path, keychain, token string, repoContext bool) {
	t.Helper()
	var contents strings.Builder
	contents.WriteString("version: 1\n")
	if keychain != "" {
		contents.WriteString("credentials:\n  keychain: " + keychain + "\n")
	}
	if token != "" {
		contents.WriteString("stacks:\n  default:\n    grafana:\n      server: https://example.invalid\n      token: " + token + "\n")
		contents.WriteString("contexts:\n  default:\n    stack: default\ncurrent-context: default\n")
	}
	if repoContext {
		contents.WriteString("contexts:\n  repo-context: {}\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents.String()), 0o600))
}

func captureKeychainPolicyStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	previous := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = previous })

	run()
	require.NoError(t, writer.Close())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return string(output)
}

// A mode is observable without inspecting implementation details: enabled
// storage migrates the fixture's plaintext token to the fake store, whereas
// disabled storage leaves that store untouched. These cases would catch a
// production change that parses credentials.keychain but applies the wrong
// trust ordering (or still permits an auto-discovered local file to opt out).
func TestLoadLayered_KeychainModePolicy(t *testing.T) {
	tests := []struct {
		name           string
		env            string
		system         string
		user           string
		local          string
		explicit       string
		load           string
		wantStored     bool
		wantWarning    bool
		wantError      string
		wantErrorPath  string
		wantEnvWarning bool
		wantLocalMerge bool
	}{
		{
			name:       "unset with no setting defaults to on",
			user:       "",
			wantStored: true,
		},
		{
			name:       "environment off wins over a trusted on setting",
			env:        "off",
			user:       "on",
			wantStored: false,
		},
		{
			name:       "environment on wins over a trusted off setting",
			env:        "on",
			user:       "off",
			wantStored: true,
		},
		{
			name:           "invalid environment keeps keychain on over trusted off",
			env:            "invalid",
			user:           "off",
			wantStored:     true,
			wantEnvWarning: true,
		},
		{
			name:       "system off disables storage",
			system:     "off",
			wantStored: false,
		},
		{
			name:       "user on overrides system off",
			system:     "off",
			user:       "on",
			wantStored: true,
		},
		{
			name:       "user off disables storage",
			user:       "off",
			wantStored: false,
		},
		{
			name:       "trusted off is case and space insensitive",
			user:       `" Off "`,
			wantStored: false,
		},
		{
			name:       "trusted on is case and space insensitive",
			user:       `" On "`,
			wantStored: true,
		},
		{
			name:        "auto discovered local off is ignored",
			local:       "off",
			wantStored:  true,
			wantWarning: true,
		},
		{
			name:           "auto discovered local invalid value is ignored",
			local:          "invalid",
			wantStored:     true,
			wantWarning:    true,
			wantLocalMerge: true,
		},
		{
			name:       "explicit flag file off wins over user and system",
			system:     "on",
			user:       "on",
			explicit:   "off",
			load:       "flag",
			wantStored: false,
		},
		{
			name:       "GCX_CONFIG file off wins over user and system",
			system:     "on",
			user:       "on",
			explicit:   "off",
			load:       "environment-file",
			wantStored: false,
		},
		{
			name:          "invalid system value names field and system source",
			system:        "invalid",
			wantError:     "credentials.keychain",
			wantErrorPath: "system",
		},
		{
			name:          "invalid user value names field and user source",
			user:          "invalid",
			wantError:     "credentials.keychain",
			wantErrorPath: "user",
		},
		{
			name:          "invalid explicit value names field and explicit source",
			system:        "on",
			user:          "off",
			explicit:      "invalid",
			load:          "flag",
			wantError:     "credentials.keychain",
			wantErrorPath: "explicit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newKeychainPolicyFixture(t)
			store := withFakeStore(t)
			t.Setenv("GCX_KEYCHAIN", test.env)

			// The user token is the observable credential for layered cases. It
			// deliberately has no credentials block when the test needs the
			// local source to be the only untrusted setting.
			if test.system != "" {
				writeKeychainPolicyConfig(t, fixture.system, test.system, "", false)
			}
			if test.user != "" || test.explicit == "" {
				writeKeychainPolicyConfig(t, fixture.user, test.user, "plaintext-user-token", false)
			}
			if test.local != "" {
				writeKeychainPolicyConfig(t, fixture.local, test.local, "", true)
			}
			if test.explicit != "" {
				writeKeychainPolicyConfig(t, fixture.explicit, test.explicit, "plaintext-explicit-token", false)
			}

			var warnings bytes.Buffer
			ctx := config.ContextWithWarningWriter(t.Context(), &warnings)
			var (
				cfg config.Config
				err error
			)
			load := func() {
				switch test.load {
				case "flag":
					cfg, err = config.LoadLayered(ctx, fixture.explicit)
				case "environment-file":
					t.Setenv(config.ConfigFileEnvVar, fixture.explicit)
					cfg, err = config.LoadLayered(ctx, "")
				default:
					cfg, err = config.LoadLayered(ctx, "")
				}
			}
			var stderr string
			if test.wantEnvWarning {
				config.ResetUnrecognisedKeychainWarningForTest()
				stderr = captureKeychainPolicyStderr(t, load)
			} else {
				load()
			}

			if test.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantError)
				assert.Contains(t, err.Error(), "accepted values are on and off")
				switch test.wantErrorPath {
				case "system":
					assert.Contains(t, err.Error(), fixture.system)
				case "user":
					assert.Contains(t, err.Error(), fixture.user)
				case "explicit":
					assert.Contains(t, err.Error(), fixture.explicit)
				default:
					t.Fatalf("missing expected source path for error case %q", test.name)
				}
				return
			}

			require.NoError(t, err)
			if test.wantStored {
				assert.Positive(t, store.sets(), "enabled keychain mode must migrate the test plaintext token")
			} else {
				assert.Zero(t, store.sets(), "disabled keychain mode must not touch the fake credential store")
			}
			if test.wantWarning {
				assert.Contains(t, warnings.String(), "credentials.keychain")
				assert.Contains(t, warnings.String(), "auto-discovered local config")
				assert.Contains(t, warnings.String(), "user or system config")
				assert.Contains(t, warnings.String(), "explicit config file")
			}
			if test.wantLocalMerge {
				require.Contains(t, cfg.Contexts, "repo-context", "filtering local credentials.keychain must retain the rest of the local layer")
				assert.Equal(t, 1, strings.Count(warnings.String(), "credentials.keychain"), warnings.String())
			}
			if test.wantEnvWarning {
				assert.Equal(t, 1, strings.Count(stderr, "warn:"), stderr)
				assert.Contains(t, stderr, "keychain storage remains enabled")
				assert.Contains(t, stderr, "GCX_KEYCHAIN=off")
			}
		})
	}
}

func TestLoadLayeredIgnoresStructurallyInvalidLocalKeychainPolicy(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "scalar", value: "false"},
		{name: "sequence", value: "[off]"},
		{name: "mapping", value: "{mode: off}"},
		{name: "null", value: "null"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newKeychainPolicyFixture(t)
			store := withFakeStore(t)
			writeKeychainPolicyConfig(t, fixture.user, "on", "plaintext-user-token", false)
			localContents := []byte("version: 1\ncredentials:\n  keychain: " + test.value + "\ncontexts:\n  repo-context: {}\n")
			require.NoError(t, os.WriteFile(fixture.local, localContents, 0o600))

			var warnings bytes.Buffer
			cfg, err := config.LoadLayered(config.ContextWithWarningWriter(t.Context(), &warnings), "")
			require.NoError(t, err)
			require.Contains(t, cfg.Contexts, "repo-context")
			assert.Positive(t, store.sets(), "trusted user on must remain effective")
			assert.Equal(t, 1, strings.Count(warnings.String(), "credentials.keychain"), warnings.String())

			raw, readErr := os.ReadFile(fixture.local)
			require.NoError(t, readErr)
			assert.Equal(t, localContents, raw, "ignored policy sanitization must not rewrite the local source")

			directCtx := config.ContextWithConfigSource(t.Context(), config.ConfigSource{Path: fixture.local, Type: "local"})
			localCfg, loadErr := config.Load(directCtx, config.ExplicitConfigFile(fixture.local))
			require.NoError(t, loadErr)
			localCfg.SetContext("added-context", false, config.Context{})
			require.NoError(t, config.Write(directCtx, config.ExplicitConfigFile(fixture.local), localCfg))
			written, readErr := os.ReadFile(fixture.local)
			require.NoError(t, readErr)
			var before, after map[string]any
			require.NoError(t, yaml.Unmarshal(localContents, &before))
			require.NoError(t, yaml.Unmarshal(written, &after))
			beforeCredentials, ok := before["credentials"].(map[string]any)
			require.True(t, ok)
			_, beforePresent := beforeCredentials["keychain"]
			assert.True(t, beforePresent)
			// gcx never honors this structurally invalid value regardless of its
			// shape, so an unrelated write drops it instead of round-tripping it
			// back through a second, key-sorting serializer.
			afterCredentials, _ := after["credentials"].(map[string]any)
			_, afterPresent := afterCredentials["keychain"]
			assert.False(t, afterPresent, "the invalid local policy value must be dropped on write, not preserved")
		})
	}
}

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

	// Isolate from the real $HOME/.config so StandardLocation doesn't find it.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", fakeConfigDir)

	cfg, err := config.Load(t.Context(), config.StandardLocation())
	req.NoError(err)

	// An empty configuration is returned
	req.Equal("default", cfg.CurrentContext)
	req.Len(cfg.Contexts, 1)
}

func TestLoad_standardLocation_withExistingConfig(t *testing.T) {
	req := require.New(t)

	fakeConfigDir := t.TempDir()

	// Isolate from the real $HOME/.config so StandardLocation doesn't find it.
	t.Setenv("HOME", t.TempDir())
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
	withFakeStore(t)
	req := require.New(t)

	configYAML := `version: 1
stacks:
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
contexts:
  default:
    stack: default
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

	// Round-trip in place. Credential-bearing entries are source-bound and
	// ordinary Write intentionally refuses to export them to another file.
	roundTripFile := configFile
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

// Write must replace the config file atomically: token persistence rewrites
// the config while other gcx invocations Load it without holding the refresh
// flock, and a truncating in-place write lets them observe a partial file
// (EOF or YAML parse error).
func TestWrite_AtomicUnderConcurrentLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	source := config.ExplicitConfigFile(configFile)

	cfg := config.Config{CurrentContext: "local"}
	require.NoError(t, config.Write(t.Context(), source, cfg))

	done := make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		defer close(done)
		for range 1000 {
			if err := config.Write(t.Context(), source, cfg); err != nil {
				writeErr <- err
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			select {
			case err := <-writeErr:
				t.Fatalf("concurrent Write failed: %v", err)
			default:
			}
			return
		default:
			_, err := config.Load(t.Context(), source)
			require.NoError(t, err, "Load must never observe a partially written config")
		}
	}
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

func TestDiscoverSources_DotConfigPreferredOverXDG(t *testing.T) {
	// When $HOME/.config has a config, it should be found even if
	// XDG_CONFIG_HOME points elsewhere (e.g. macOS ~/Library/Application Support).
	homeDir := t.TempDir()
	xdgDir := t.TempDir() // simulates platform XDG dir (e.g. ~/Library/Application Support)

	// Create config in $HOME/.config/gcx/.
	dotConfigFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dotConfigFile), 0o755))
	require.NoError(t, os.WriteFile(dotConfigFile, []byte("contexts:\n  dot: {}\ncurrent-context: dot\n"), 0o600))

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir) // empty, no config

	sources, err := config.DiscoverSources(
		config.WithSystemDir(t.TempDir()),
		config.WithWorkDir(t.TempDir()),
	)
	require.NoError(t, err)

	require.Len(t, sources, 1)
	assert.Equal(t, "user", sources[0].Type)
	assert.Equal(t, dotConfigFile, sources[0].Path)
}

func TestDiscoverSources_FallsBackToXDGWhenDotConfigMissing(t *testing.T) {
	xdgDir := t.TempDir()

	// Put config only in the XDG dir.
	xdgFile := filepath.Join(xdgDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(xdgFile), 0o755))
	require.NoError(t, os.WriteFile(xdgFile, []byte("contexts:\n  xdg: {}\ncurrent-context: xdg\n"), 0o600))

	// HOME points to a dir with no .config/gcx/ at all.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	sources, err := config.DiscoverSources(
		config.WithSystemDir(t.TempDir()),
		config.WithWorkDir(t.TempDir()),
	)
	require.NoError(t, err)

	require.Len(t, sources, 1)
	assert.Equal(t, "user", sources[0].Type)
	assert.Equal(t, xdgFile, sources[0].Path)
}

func TestCheckDuplicateUserConfig_BothExist(t *testing.T) {
	homeDir := t.TempDir()
	xdgDir := t.TempDir()

	// Create config in both $HOME/.config/gcx/ and XDG dir.
	dotConfigFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dotConfigFile), 0o755))
	require.NoError(t, os.WriteFile(dotConfigFile, []byte("contexts:\n  x: {}\n"), 0o600))

	xdgFile := filepath.Join(xdgDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(xdgFile), 0o755))
	require.NoError(t, os.WriteFile(xdgFile, []byte("contexts:\n  x: {}\n"), 0o600))

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	dup := config.CheckDuplicateUserConfig()
	require.NotNil(t, dup)
	assert.Equal(t, dotConfigFile, dup.Active)
	assert.Equal(t, xdgFile, dup.Ignored)
}

// isolatedLoaderEnv isolates HOME and XDG_CONFIG_HOME so source discovery only
// sees files the test creates. Returns the user-config dir and working dir.
func isolatedLoaderEnv(t *testing.T) (string, string) {
	t.Helper()
	userDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("GCX_CONFIG", "")

	t.Chdir(workDir)
	return userDir, workDir
}

func writeLoaderConfig(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestLoadForWrite_explicitFile(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	userPath := filepath.Join(userDir, "gcx", "config.yaml")
	writeLoaderConfig(t, userPath, "current-context: dev\ncontexts:\n  dev: {}\n")

	cfg, src, err := config.LoadForWrite(t.Context(), userPath, "")
	require.NoError(t, err)
	require.Equal(t, "dev", cfg.CurrentContext)

	filename, err := src()
	require.NoError(t, err)
	require.Equal(t, userPath, filename)
}

func TestCanInitializeMissingSourceOnlyAcceptsInitialAbsentTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-config.yaml")
	cfg, _, err := config.LoadForWrite(t.Context(), path, "")
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.True(t, config.CanInitializeMissingSource(cfg, err))

	lateReadErr := &os.PathError{Op: "read", Path: path, Err: os.ErrNotExist}
	assert.False(t, config.CanInitializeMissingSource(config.Config{}, lateReadErr),
		"an unrelated or post-read ENOENT must not authorize constructive initialization")
}

func TestLoadForWrite_fileType_targetsNamedLayer(t *testing.T) {
	userDir, workDir := isolatedLoaderEnv(t)
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"contexts:\n  user-ctx: {}\ncurrent-context: user-ctx\n")
	writeLoaderConfig(t, filepath.Join(workDir, ".gcx.yaml"),
		"contexts:\n  local-ctx: {}\ncurrent-context: local-ctx\n")

	cfg, _, err := config.LoadForWrite(t.Context(), "", "local")
	require.NoError(t, err)
	require.Equal(t, "local-ctx", cfg.CurrentContext)
	require.Contains(t, cfg.Contexts, "local-ctx")
	require.NotContains(t, cfg.Contexts, "user-ctx",
		"LoadForWrite must not merge other layers into the result")
}

func TestLoadForWrite_fileType_notFound_errors(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"current-context: dev\ncontexts:\n  dev: {}\n")

	_, _, err := config.LoadForWrite(t.Context(), "", "local")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no local config file found")
}

func TestLoadForWrite_fileType_user_freshSystem_autoCreates(t *testing.T) {
	isolatedLoaderEnv(t)

	// Fresh system: no config files exist anywhere. --file user must auto-create
	// the user config (preserving the pre-perf LoadLayered behavior) rather than
	// erroring with "no user config file found".
	_, src, err := config.LoadForWrite(t.Context(), "", "user")
	require.NoError(t, err)
	require.NotNil(t, src)

	filename, err := src()
	require.NoError(t, err)
	require.FileExists(t, filename)
}

func TestLoadForWrite_fileType_nonUser_freshSystem_errors(t *testing.T) {
	isolatedLoaderEnv(t)

	// Auto-create only ever applied to the user layer, so on a fresh system a
	// non-user --file target still errors instead of conjuring a file.
	_, _, err := config.LoadForWrite(t.Context(), "", "local")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no local config file found")
}

func TestLoadForWrite_singleSource_autoDetects(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"current-context: dev\ncontexts:\n  dev: {}\n")

	cfg, _, err := config.LoadForWrite(t.Context(), "", "")
	require.NoError(t, err)
	require.Equal(t, "dev", cfg.CurrentContext)
}

func TestLoadDiagnostics_NoConfigReturnsNil(t *testing.T) {
	isolatedLoaderEnv(t)
	assert.Nil(t, config.LoadDiagnostics(t.Context()))
}

func TestLoadDiagnostics_ReadsEnabledFlag(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"diagnostics:\n  agent-invocation-log: true\n  log-dir: /user/logs\ncurrent-context: dev\ncontexts:\n  dev: {}\n")

	d := config.LoadDiagnostics(t.Context())
	require.NotNil(t, d)
	assert.True(t, d.AgentInvocationLog)
	assert.Equal(t, "/user/logs", d.LogDir)
}

func TestLoadDiagnostics_LayersLocalOverUser(t *testing.T) {
	userDir, workDir := isolatedLoaderEnv(t)
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"diagnostics:\n  agent-invocation-log: true\n  log-dir: /user/logs\n")
	writeLoaderConfig(t, filepath.Join(workDir, ".gcx.yaml"),
		"diagnostics:\n  log-dir: /local/logs\n")

	d := config.LoadDiagnostics(t.Context())
	require.NotNil(t, d)
	assert.True(t, d.AgentInvocationLog, "feature stays enabled from the user layer")
	assert.Equal(t, "/local/logs", d.LogDir, "local layer overrides log-dir")
}

func TestLoadForWrite_multipleSources_errors(t *testing.T) {
	userDir, workDir := isolatedLoaderEnv(t)
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"current-context: dev\ncontexts:\n  dev: {}\n")
	writeLoaderConfig(t, filepath.Join(workDir, ".gcx.yaml"),
		"current-context: local\ncontexts:\n  local: {}\n")

	_, _, err := config.LoadForWrite(t.Context(), "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--file")
}

func TestCheckDuplicateUserConfig_NoDuplicate(t *testing.T) {
	homeDir := t.TempDir()
	xdgDir := t.TempDir()

	// Config only in $HOME/.config.
	dotConfigFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dotConfigFile), 0o755))
	require.NoError(t, os.WriteFile(dotConfigFile, []byte("contexts:\n  x: {}\n"), 0o600))

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir) // empty, no config

	dup := config.CheckDuplicateUserConfig()
	assert.Nil(t, dup)
}
