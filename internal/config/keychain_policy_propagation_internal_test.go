package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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

	cfg, err := Load(withKeychainPolicy(t.Context(), policy), ExplicitConfigFile(path))
	require.NoError(t, err)
	require.Equal(t, policy, cfg.keychainPolicy)
	require.Equal(t, policy, cfg.Contexts["default"].keychainPolicy)
}
