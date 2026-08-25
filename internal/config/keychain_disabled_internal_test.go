package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A deliberate opt-out must not inherit the transient-outage protection that
// refuses to replace a credential still holding a keychain reference: the user
// asked for plaintext, and erroring would leave them unable to log in at all.
func TestBoundKeychainDisabledReplacesReferencedCredentialWithPlaintext(t *testing.T) {
	store := newBoundTestStore()
	store.getErr = credentials.ErrDisabled
	store.setErr = credentials.ErrDisabled
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	account := credentials.BoundAccountKey(binding)
	store.entries[account] = "keychain-token"
	sentinel := credentials.FormatBoundSentinel(binding)
	writeBoundTestYAML(t, path, server, "token", sentinel)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Empty(t, cfg.Stacks["default"].Grafana.APIToken, "a disabled keychain must not resolve the sentinel")

	cfg.Stacks["default"].Grafana.APIToken = "replacement-token"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "replacement-token")
	assert.NotContains(t, string(raw), "keychain:")
	assert.Equal(t, map[string]string{account: "keychain-token"}, store.entries,
		"the abandoned generation must be left intact, not deleted through a disabled store")
	assert.Empty(t, store.deletes)
}

func TestBoundKeychainDisabledFallbackWarningOmitsTroubleshootingHint(t *testing.T) {
	logger := &boundTestLogger{}
	var warnings strings.Builder
	txn := newKeychainWriteTransaction(newBoundTestStore(), logger)
	txn.warnUnavailableOnce = func(emit func()) { emit() }
	txn.plaintextFallback = true
	txn.fallbackErr = credentials.ErrDisabled

	require.NoError(t, txn.commit(&warnings))
	assert.Equal(t, "Warning: keychain storage is disabled; credentials remain in plaintext on disk; "+
		"unset GCX_KEYCHAIN to store credentials in the OS credential store\n",
		warnings.String())
	assert.NotContains(t, warnings.String(), "is available and working",
		"a deliberate opt-out must not be reported as a broken credential store")
}
