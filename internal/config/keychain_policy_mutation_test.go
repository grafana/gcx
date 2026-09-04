package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	commandconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sourceIdentityChangingContext struct {
	onDone func()
	once   sync.Once
}

func (*sourceIdentityChangingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *sourceIdentityChangingContext) Done() <-chan struct{} {
	c.once.Do(c.onDone)
	return nil
}

func (*sourceIdentityChangingContext) Err() error    { return nil }
func (*sourceIdentityChangingContext) Value(any) any { return nil }

type policyMutationStore struct {
	entries map[string]string
	err     error
	calls   int
}

func (s *policyMutationStore) Get(key string) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	value, ok := s.entries[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return value, nil
}

func (s *policyMutationStore) Set(key, value string) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	s.entries[key] = value
	return nil
}

func (s *policyMutationStore) Delete(key string) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	delete(s.entries, key)
	return nil
}

func TestSetCommandMutatesKeychainPolicyUsingIntendedPolicyAtomically(t *testing.T) {
	tests := []struct {
		name           string
		initialMode    string
		intendedMode   string
		storeErr       error
		wantStoreCalls bool
		wantPlaintext  bool
		wantSentinel   bool
		legacy         bool
	}{
		{
			name:          "on to off succeeds with unavailable store",
			initialMode:   "on",
			intendedMode:  "off",
			storeErr:      credentials.ErrUnavailable,
			wantPlaintext: true,
		},
		{
			name:          "on to off skips available store",
			initialMode:   "on",
			intendedMode:  "off",
			wantPlaintext: true,
		},
		{
			name:          "legacy to off migrates without touching available store",
			intendedMode:  "off",
			wantPlaintext: true,
			legacy:        true,
		},
		{
			name:           "off to on stores credential in same mutation",
			initialMode:    "off",
			intendedMode:   "on",
			wantStoreCalls: true,
			wantSentinel:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newKeychainPolicyFixture(t)
			path := fixture.explicit
			contents := `version: 1
credentials:
  keychain: ` + test.initialMode + `
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: existing-plaintext-token
contexts:
  default:
    stack: default
current-context: default
`
			if test.legacy {
				contents = `contexts:
  default:
    grafana:
      server: https://example.invalid
      token: existing-plaintext-token
current-context: default
`
			}
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			store := &policyMutationStore{
				entries: map[string]string{},
				err:     test.storeErr,
			}
			restore := config.SetKeychainStoreFnForTest(func() credentials.Store { return store })
			t.Cleanup(restore)

			cmd := commandconfig.Command()
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{
				"set", "--config", path,
				"credentials.keychain", test.intendedMode,
			})
			err := cmd.ExecuteContext(t.Context())
			require.NoError(t, err)

			raw, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, test.wantStoreCalls, store.calls > 0)
			assert.Equal(t, test.wantPlaintext, strings.Contains(string(raw), "existing-plaintext-token"))
			assert.Equal(t, test.wantSentinel, strings.Contains(string(raw), "keychain:gcx:v2:"))
			assert.Contains(t, string(raw), `keychain: "`+test.intendedMode+`"`)
		})
	}
}

func TestSetKeychainPolicyRejectsAutoDiscoveredLocalTarget(t *testing.T) {
	tests := []struct {
		name     string
		fileType string
	}{
		{name: "sole discovered source is local"},
		{name: "explicit --file local", fileType: "local"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newKeychainPolicyFixture(t)
			writeKeychainPolicyConfig(t, fixture.local, "on", "", true)

			store := &policyMutationStore{entries: map[string]string{}}
			restore := config.SetKeychainStoreFnForTest(func() credentials.Store { return store })
			t.Cleanup(restore)

			before, readErr := os.ReadFile(fixture.local)
			require.NoError(t, readErr)

			_, mutateErr := config.SetKeychainPolicy(t.Context(), "", test.fileType, "off")
			require.Error(t, mutateErr)
			assert.Contains(t, mutateErr.Error(), "local")
			assert.Contains(t, mutateErr.Error(), "--file user")
			assert.Contains(t, mutateErr.Error(), "--file system")
			assert.Contains(t, mutateErr.Error(), "--config")

			after, readErr := os.ReadFile(fixture.local)
			require.NoError(t, readErr)
			assert.Equal(t, before, after, "the auto-discovered local file must be left untouched")
			assert.Zero(t, store.calls, "no keychain write should happen when the mutation is rejected")
		})
	}
}

func TestSetKeychainPolicyMultiSourceAmbiguityExcludesLocal(t *testing.T) {
	fixture := newKeychainPolicyFixture(t)
	writeKeychainPolicyConfig(t, fixture.system, "on", "", false)
	writeKeychainPolicyConfig(t, fixture.user, "on", "", false)
	writeKeychainPolicyConfig(t, fixture.local, "off", "", true)

	_, err := config.SetKeychainPolicy(t.Context(), "", "", "off")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--file (system, user)")
	assert.NotContains(t, err.Error(), "--file (system, user, local)")
}

func TestClearKeychainPolicyRunsLockedTransaction(t *testing.T) {
	fixture := newKeychainPolicyFixture(t)
	writeKeychainPolicyConfig(t, fixture.user, "off", "", false)

	store := &policyMutationStore{entries: map[string]string{}}
	restore := config.SetKeychainStoreFnForTest(func() credentials.Store { return store })
	t.Cleanup(restore)

	source, err := config.ClearKeychainPolicy(t.Context(), "", "")
	require.NoError(t, err)
	path, pathErr := source()
	require.NoError(t, pathErr)
	require.Equal(t, fixture.user, path)

	raw, readErr := os.ReadFile(fixture.user)
	require.NoError(t, readErr)
	assert.NotContains(t, string(raw), "keychain:")
}

func TestClearKeychainPolicyRejectsAutoDiscoveredLocalTarget(t *testing.T) {
	fixture := newKeychainPolicyFixture(t)
	writeKeychainPolicyConfig(t, fixture.local, "off", "", true)

	before, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)

	_, err := config.ClearKeychainPolicy(t.Context(), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local")

	after, readErr := os.ReadFile(fixture.local)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
}

func TestSetKeychainPolicyRejectsSourceIdentityChangeAfterLockSelection(t *testing.T) {
	home := t.TempDir()
	xdgHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	t.Setenv("GCX_KEYCHAIN", "")
	t.Chdir(t.TempDir())

	fallbackPath := filepath.Join(xdgHome, config.StandardConfigFolder, config.StandardConfigFileName)
	preferredPath := filepath.Join(home, ".config", config.StandardConfigFolder, config.StandardConfigFileName)
	fallbackContents := []byte("version: 1\ncontexts:\n  fallback: {}\ncurrent-context: fallback\n")
	preferredContents := []byte("version: 1\ncontexts:\n  preferred: {}\ncurrent-context: preferred\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(fallbackPath), 0o755))
	require.NoError(t, os.WriteFile(fallbackPath, fallbackContents, 0o600))

	ctx := &sourceIdentityChangingContext{
		onDone: func() {
			require.NoError(t, os.MkdirAll(filepath.Dir(preferredPath), 0o755))
			require.NoError(t, os.WriteFile(preferredPath, preferredContents, 0o600))
		},
	}

	_, err := config.SetKeychainPolicy(ctx, "", "user", "off")
	require.ErrorContains(t, err, "config write lock identity changed")

	fallbackAfter, readErr := os.ReadFile(fallbackPath)
	require.NoError(t, readErr)
	assert.Equal(t, fallbackContents, fallbackAfter)
	preferredAfter, readErr := os.ReadFile(preferredPath)
	require.NoError(t, readErr)
	assert.Equal(t, preferredContents, preferredAfter, "a rediscovered owner must not be written under another source's lock")
}
