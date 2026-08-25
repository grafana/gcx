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

func TestResolveKeychainModeSkipsConfigReadWhenEnvironmentDecides(t *testing.T) {
	read := 0
	configValue := func() string {
		read++
		return ""
	}
	resolveKeychainMode(func(string) string { return "disabled" }, configValue)
	assert.Zero(t, read, "a config-file read must not be paid when the environment already decides")
}

// A checked-in .gcx.yaml must not be able to move a user's credentials into
// plaintext; the user and system layers may (docs/adrs/config-v1/001).
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

func TestKeychainModeConfigValueHonoursExplicitConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeKeychainModeConfig(t, path, "disabled")
	t.Setenv(ConfigFileEnvVar, path)

	assert.Equal(t, "disabled", keychainModeConfigValue(t.Context()))
}

func TestReadCredentialsHonoursLegacyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `contexts:
  default:
    grafana:
      server: https://example.invalid
current-context: default
credentials:
  keychain: disabled
`
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	creds, err := readCredentials(path)
	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "disabled", creds.Keychain)
}

// The docs generator reads the struct tag; resolveKeychainMode reads the
// constant. They must name the same variable.
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
