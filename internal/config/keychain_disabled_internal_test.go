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

// Binds the disclosure to the real reconcile rather than to a hand-built
// transaction. It calls reconcileKeychain directly because the commit warning
// runs behind a process-wide sync.Once that any earlier test may consume.
func TestReconcileMarksTheAbandonedGenerationWhenDisabled(t *testing.T) {
	store := newBoundTestStore()
	store.getErr = credentials.ErrDisabled
	store.setErr = credentials.ErrDisabled
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	store.entries[credentials.BoundAccountKey(binding)] = "keychain-token"
	writeBoundTestYAML(t, path, server, "token", credentials.FormatBoundSentinel(binding))

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.APIToken = "replacement-token"

	txn, err := reconcileKeychain(&cfg, store, &boundTestLogger{})
	require.NoError(t, err)
	assert.True(t, txn.abandonedGeneration,
		"replacing a referenced credential through a disabled store abandons the old generation")
	assert.True(t, txn.plaintextFallback)
	assert.Empty(t, txn.deletes, "a disabled store cannot delete the old account")
}

// Section 2 of the review: the replaced generation cannot be deleted through a
// disabled store, so the warning has to disclose that it stays behind.
func TestBoundKeychainDisabledWarningDisclosesTheAbandonedGeneration(t *testing.T) {
	logger := &boundTestLogger{}
	var warnings strings.Builder
	txn := newKeychainWriteTransaction(newBoundTestStore(), logger)
	txn.warnUnavailableOnce = func(emit func()) { emit() }
	txn.plaintextFallback = true
	txn.fallbackErr = credentials.ErrDisabled
	txn.abandonedGeneration = true

	require.NoError(t, txn.commit(&warnings))
	assert.Contains(t, warnings.String(), "is still in the OS credential store and gcx can no longer reach it")
	assert.Contains(t, warnings.String(), "delete the stale gcx entry")
}

func TestKeychainReadRejectionReasonNamesTheDeliberateOptOut(t *testing.T) {
	assert.Equal(t, "keychain use is disabled by GCX_KEYCHAIN", keychainReadRejectionReason(credentials.ErrDisabled))
	assert.Equal(t, "the OS keychain is locked", keychainReadRejectionReason(credentials.ErrLocked),
		"ErrDisabled wraps ErrUnavailable, so its branch must not shadow the locked one")
	assert.Equal(t, "the OS keychain could not be read", keychainReadRejectionReason(credentials.ErrUnavailable))
}

// Deleting cannot silently succeed while the store is disabled, but the error
// must name a repair that works when the store is permanently unreachable.
func TestBoundKeychainDisabledDeleteNamesAWorkingRepair(t *testing.T) {
	store := newBoundTestStore()
	store.getErr = credentials.ErrDisabled
	txn := newKeychainWriteTransaction(store, &boundTestLogger{})
	txn.deferDelete("v2:some-account", "stack:default", credentials.FieldGrafanaToken)

	err := txn.preflightDeletes()
	require.ErrorIs(t, err, credentials.ErrDisabled)
	assert.Contains(t, err.Error(), "edit the config file to remove the reference")
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
	assert.NotContains(t, warnings.String(), "is available and working",
		"a deliberate opt-out must not be reported as a broken credential store")
}
