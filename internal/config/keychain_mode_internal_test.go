package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercises the real GCX_KEYCHAIN read, not an injected getenv: without this,
// dropping the environment lookup would leave every other test passing.
func TestKeychainModeForProcess(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want keychainMode
	}{
		{name: "unset defaults to enabled", want: keychainModeEnabled},
		{name: "disabled", env: "disabled", want: keychainModeDisabled},
		{name: "off", env: "off", want: keychainModeDisabled},
		{name: "false", env: "false", want: keychainModeDisabled},
		{name: "zero", env: "0", want: keychainModeDisabled},
		{name: "no, the falsy value internal/agent accepts", env: "no", want: keychainModeDisabled},
		{name: "case and space insensitive", env: " Disabled ", want: keychainModeDisabled},
		{name: "enabled", env: "enabled", want: keychainModeEnabled},
		{name: "typo fails toward encrypted storage", env: "disabledd", want: keychainModeEnabled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(envKeychain, test.env)

			assert.Equal(t, test.want, keychainModeForProcess())
		})
	}
}

// The generated environment-variable reference comes from the struct tag while
// keychainModeForProcess reads the constant. They must name the same variable.
func TestKeychainEnvTagMatchesResolvedName(t *testing.T) {
	field, ok := reflect.TypeFor[CLIOptions]().FieldByName("Keychain")
	require.True(t, ok)
	assert.Equal(t, envKeychain, strings.Split(field.Tag.Get("env"), ",")[0])
}

// An unrelated malformed variable must not decide this one: LoadCLIOptions
// fails on the first bad field, which would resolve an explicit opt-out to
// enabled.
func TestKeychainModeIgnoresUnrelatedMalformedOptions(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "not-a-bool")
	t.Setenv(envKeychain, "disabled")

	_, err := LoadCLIOptions()
	require.Error(t, err, "guards the premise: a bad bool must still fail CLI option parsing")
	assert.Equal(t, keychainModeDisabled, keychainModeForProcess())
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
