package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveKeychainMode(t *testing.T) {
	tests := []struct {
		name        string
		env         string
		configValue string
		want        keychainMode
	}{
		{name: "unset defaults to enabled", want: keychainModeEnabled},
		{name: "env disabled", env: "disabled", want: keychainModeDisabled},
		{name: "env off", env: "off", want: keychainModeDisabled},
		{name: "env false", env: "false", want: keychainModeDisabled},
		{name: "env 0", env: "0", want: keychainModeDisabled},
		{name: "env is case and space insensitive", env: " Disabled ", want: keychainModeDisabled},
		{name: "env enabled", env: "enabled", want: keychainModeEnabled},
		{name: "env typo fails toward encrypted storage", env: "disabledd", want: keychainModeEnabled},
		{name: "config disabled", configValue: "disabled", want: keychainModeDisabled},
		{name: "config typo fails toward encrypted storage", configValue: "of", want: keychainModeEnabled},
		{name: "env beats config", env: "enabled", configValue: "disabled", want: keychainModeEnabled},
		{name: "env beats config both ways", env: "disabled", configValue: "enabled", want: keychainModeDisabled},
		{name: "empty env falls through to config", env: "", configValue: "disabled", want: keychainModeDisabled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(name string) string {
				require.Equal(t, envKeychain, name)
				return test.env
			}
			assert.Equal(t, test.want, resolveKeychainMode(getenv, func() string { return test.configValue }))
		})
	}
}

func TestKeychainModeConfigValueIgnoresRepositoryLocalLayer(t *testing.T) {
	userDir := t.TempDir()
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(userDir, StandardConfigFolder), 0o700))
	userPath := filepath.Join(userDir, StandardConfigFolder, StandardConfigFileName)
	localPath := filepath.Join(workDir, LocalConfigFileName)

	writeKeychainModeConfig(t, localPath, "disabled")
	writeKeychainModeConfig(t, userPath, "")
	t.Setenv(ConfigFileEnvVar, "")
	t.Chdir(workDir)
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("HOME", userDir)

	assert.Empty(t, keychainModeConfigValue(t.Context()), "the local layer must not switch the keychain off")

	writeKeychainModeConfig(t, userPath, "disabled")
	assert.Equal(t, "disabled", keychainModeConfigValue(t.Context()))
}

// keychainModeForProcess is the expression the production memoization calls, so
// these cases cover the real os.Getenv against a real config file rather than
// an injected getenv. Without them, dropping the environment check would leave
// every other test passing.
func TestKeychainModeForProcessAlwaysHonoursTheEnvironment(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		fileValue  string
		wantMode   keychainMode
		wantConfig string
	}{
		{name: "env disables against a file that enables", env: "disabled", fileValue: "enabled", wantMode: keychainModeDisabled},
		{name: "env enables against a file that disables", env: "enabled", fileValue: "disabled", wantMode: keychainModeEnabled},
		{name: "file decides when the env is unset", env: "", fileValue: "disabled", wantMode: keychainModeDisabled},
		{name: "env decides with no file value", env: "disabled", fileValue: "", wantMode: keychainModeDisabled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeKeychainModeConfig(t, path, test.fileValue)
			t.Setenv(ConfigFileEnvVar, path)
			t.Setenv(envKeychain, test.env)

			assert.Equal(t, test.wantMode, keychainModeForProcess(t.Context()))
		})
	}
}

// The primary CI case: an ephemeral box with no config file at all.
func TestKeychainModeForProcessHonoursTheEnvironmentWithNoConfigFile(t *testing.T) {
	empty := t.TempDir()
	t.Setenv(ConfigFileEnvVar, "")
	t.Chdir(empty)
	t.Setenv("XDG_CONFIG_HOME", empty)
	t.Setenv("HOME", empty)
	t.Setenv(envKeychain, "disabled")

	assert.Equal(t, keychainModeDisabled, keychainModeForProcess(t.Context()))
}

// An unreadable or malformed config file must not strand the environment's
// answer: the env var is checked before the file is touched.
func TestKeychainModeForProcessHonoursTheEnvironmentOverAnUnreadableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\nnot: valid: yaml: here\n"), 0o600))
	t.Setenv(ConfigFileEnvVar, path)
	t.Setenv(envKeychain, "disabled")

	assert.Equal(t, keychainModeDisabled, keychainModeForProcess(t.Context()))
}

func TestKeychainModeConfigValueHonoursExplicitConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeKeychainModeConfig(t, path, "disabled")
	t.Setenv(ConfigFileEnvVar, path)

	assert.Equal(t, "disabled", keychainModeConfigValue(t.Context()))
}

func TestKeychainEnvTagMatchesResolvedName(t *testing.T) {
	field, ok := reflect.TypeFor[CredentialsConfig]().FieldByName("Keychain")
	require.True(t, ok)
	assert.Equal(t, envKeychain, strings.Split(field.Tag.Get("env"), ",")[0])
}

// The disabled mode must never reach credentials.Open, so this asserts the
// store selection rather than the probe. The enabled branch is deliberately
// untested here: it would probe the real OS keychain.
func TestKeychainStoreForDisabledModeNeverTouchesTheOSKeychain(t *testing.T) {
	store := keychainStoreForMode(keychainModeDisabled)

	_, err := store.Get("any-account")
	require.ErrorIs(t, err, credentials.ErrDisabled)
	require.ErrorIs(t, store.Set("any-account", "value"), credentials.ErrDisabled)
	require.ErrorIs(t, store.Delete("any-account"), credentials.ErrDisabled)
}

func writeKeychainModeConfig(t *testing.T, path, keychain string) {
	t.Helper()
	contents := `version: 1
contexts:
  default: {}
current-context: default
`
	if keychain != "" {
		contents += "credentials:\n  keychain: " + keychain + "\n"
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}
