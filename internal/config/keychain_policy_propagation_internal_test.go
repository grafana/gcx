package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoadRetainsEffectiveKeychainPolicyAfterPlaintextMigrationRefresh pins
// that a policy handed to the load as an option survives everything the load
// does afterwards, including the post-write runtime refresh, and reaches the
// resolved context that REST config construction reads. Losing it there would
// let an asynchronous OAuth refresh persist under a different policy than the
// command it belongs to.
func TestLoadRetainsEffectiveKeychainPolicyAfterPlaintextMigrationRefresh(t *testing.T) {
	withFakeKeychain(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
credentials:
  keychain: off
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: plaintext-token
contexts:
  default:
    stack: default
current-context: default
`), 0o600))
	policy := keychainPolicy{mode: keychainModeEnabled, source: "higher-priority-policy"}

	cfg, err := load(t.Context(), ExplicitConfigFile(path), loadOptions{}.withKeychainPolicy(policy))
	require.NoError(t, err)
	require.Equal(t, policy, cfg.keychainPolicy)
	require.Equal(t, policy, cfg.Contexts["default"].keychainPolicy)
}

// TestLoadUnderResolvedPolicyReloadsSelectedOwnerUnderTrustedOptOut is the
// property the exported entry point exists for: the reloaded file declares no
// policy of its own, so a plain Load would resolve the default "on" and push
// its plaintext into the credential store, contradicting the trusted opt-out
// the caller already resolved from another layer.
func TestLoadUnderResolvedPolicyReloadsSelectedOwnerUnderTrustedOptOut(t *testing.T) {
	store := withFakeKeychain(t)
	t.Setenv(envKeychain, "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: plaintext-token
contexts:
  default:
    stack: default
current-context: default
`)
	require.NoError(t, os.WriteFile(path, contents, 0o600))

	resolved := Config{keychainPolicy: keychainPolicy{mode: keychainModeDisabled, source: "trusted-user-config"}}
	cfg, err := LoadUnderResolvedPolicy(t.Context(), ExplicitConfigFile(path), resolved)
	require.NoError(t, err)
	require.Equal(t, resolved.keychainPolicy, cfg.keychainPolicy)
	require.Zero(t, store.calls, "a resolved opt-out must keep the reload away from the credential store")

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, contents, after, "the reload must not migrate plaintext under a trusted opt-out")
}
