package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
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

// A disabled store cannot delete the replaced generation, so the warning must
// disclose that it remains in the OS credential store.
func TestBoundKeychainDisabledWarningDisclosesTheAbandonedGeneration(t *testing.T) {
	logger := &boundTestLogger{}
	var warnings strings.Builder
	txn := newKeychainWriteTransaction(newBoundTestStore(), logger)
	// Suppress the generic availability warning. The abandoned-generation
	// repair must still reach the caller because it is more actionable.
	txn.warnUnavailableOnce = func(func()) {}
	txn.plaintextFallback = true
	txn.fallbackErr = credentials.ErrDisabled
	txn.abandonedGeneration = true

	require.NoError(t, txn.commit(&warnings))
	assert.Equal(t, "warn: keychain storage is disabled; the old keychain item cannot be removed while disabled and credentials remain in plaintext on disk; "+
		"enable keychain storage later, then remove the stale gcx entry through your OS credential store\n", warnings.String())
}

func TestBoundKeychainDisabledWarningUsesTypedAgentEvent(t *testing.T) {
	previousAgentMode := agent.IsAgentMode()
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(previousAgentMode) })

	var warnings bytes.Buffer
	txn := newKeychainWriteTransaction(newBoundTestStore(), &boundTestLogger{})
	txn.warnUnavailableOnce = func(func()) {}
	txn.plaintextFallback = true
	txn.fallbackErr = credentials.ErrDisabled
	txn.abandonedGeneration = true

	require.NoError(t, txn.commit(&warnings))
	var event struct {
		Class   string `json:"class"`
		Summary string `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(warnings.Bytes(), &event))
	assert.Equal(t, "warning", event.Class)
	assert.Contains(t, event.Summary, "old keychain item cannot be removed while disabled")
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

// The delete guard applies only to credentials that are in the store. A
// plaintext credential has no keychain state, so nothing is queued for deletion
// and the write must go through untouched.
func TestBoundKeychainDisabledUnsetsAPlaintextCredentialNormally(t *testing.T) {
	store := newBoundTestStore()
	store.getErr = credentials.ErrDisabled
	store.setErr = credentials.ErrDisabled
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeBoundTestYAML(t, path, "https://example.invalid", "token", "plaintext-token")

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	require.Equal(t, "plaintext-token", cfg.Stacks["default"].Grafana.APIToken)

	cfg.Stacks["default"].Grafana.APIToken = ""
	require.True(t, cfg.MarkSecretPathMutation("stacks.default.grafana.token"))
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "plaintext-token")
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
	assert.Equal(t, "warn: keychain storage is disabled; credentials remain in plaintext on disk; "+
		"enable keychain storage to store credentials in the OS credential store\n",
		warnings.String())
	assert.NotContains(t, warnings.String(), "is available and working",
		"a deliberate opt-out must not be reported as a broken credential store")
}
