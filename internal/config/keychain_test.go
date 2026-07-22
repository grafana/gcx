package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is an in-memory credentials.Store for keychain integration tests.
// The default store under `go test` is a no-op that returns ErrUnavailable;
// tests that exercise the keychain path install a fakeStore via withFakeStore.
type fakeStore struct {
	mu       sync.Mutex
	entries  map[string]string
	setCalls int
	deletes  []string
	// getErr, when non-nil, is returned by every Get to simulate a keychain
	// that is reachable for writes but cannot resolve reads (e.g. a locked
	// session). Tests set it to credentials.ErrUnavailable.
	getErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[string]string{}}
}

func (s *fakeStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return "", s.getErr
	}
	v, ok := s.entries[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return v, nil
}

func (s *fakeStore) setGetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr = err
}

func (s *fakeStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = value
	s.setCalls++
	return nil
}

func (s *fakeStore) sets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setCalls
}

func (s *fakeStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, key)
	delete(s.entries, key)
	return nil
}

func (s *fakeStore) deleted(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.deletes, key)
}

func (s *fakeStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// withFakeStore installs a fakeStore for the duration of the test and returns
// it so the test can assert on its contents.
func withFakeStore(t *testing.T) *fakeStore {
	t.Helper()
	store := newFakeStore()
	restore := config.SetKeychainStoreFnForTest(func() credentials.Store { return store })
	t.Cleanup(restore)
	return store
}

func testStackBinding(t *testing.T, path, name, server string, field credentials.Field) credentials.Binding {
	t.Helper()
	binding, err := config.StackBindingForTest(path, name, server, field)
	require.NoError(t, err)
	return binding
}

func testCloudBinding(t *testing.T, path, name string, field credentials.Field) credentials.Binding {
	t.Helper()
	binding, err := config.CloudBindingForTest(path, name, field)
	require.NoError(t, err)
	return binding
}

func storeBoundReference(t *testing.T, store *fakeStore, binding credentials.Binding, value string) credentials.BoundReference {
	t.Helper()
	ref, err := credentials.NewBoundReference(binding)
	require.NoError(t, err)
	require.NoError(t, store.Set(ref.Account, value))
	return ref
}

func storedBoundValue(t *testing.T, store *fakeStore, binding credentials.Binding, value string) string {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	var matches []string
	for account, stored := range store.entries {
		if credentials.MatchesBoundAccount(account, binding) && stored == value {
			matches = append(matches, account)
		}
	}
	require.Len(t, matches, 1)
	return matches[0]
}

func writeYAML(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestLoad_NoSecretsDoesNotOpenKeychain(t *testing.T) {
	var opens int
	store := newFakeStore()
	restore := config.SetKeychainStoreFnForTest(func() credentials.Store {
		opens++
		return store
	})
	t.Cleanup(restore)

	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)

	_, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Equal(t, 0, opens, "keychain must not be opened when there are no secrets to resolve or migrate")
}

func TestLoad_SentinelOpensKeychain(t *testing.T) {
	var opens int
	store := newFakeStore()
	restore := config.SetKeychainStoreFnForTest(func() credentials.Store {
		opens++
		return store
	})
	t.Cleanup(restore)

	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	binding := testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldGrafanaToken)
	ref := storeBoundReference(t, store, binding, "resolved-token")
	contents := fmt.Sprintf(`version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: %s
contexts:
  default:
    stack: default
current-context: default
`, ref.Sentinel)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	opens = 0

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.GreaterOrEqual(t, opens, 1, "keychain must be opened to resolve a sentinel")
	assert.Equal(t, "resolved-token", cfg.Contexts["default"].Grafana.APIToken)
}

func TestLoad_MigratesPlaintextSecretsIntoKeychain(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: plain-svc-token
      password: plain-password
      oauth-token: gat_plain
      oauth-refresh-token: gar_plain
cloud:
  grafana-com:
    token: plain-cloud-token
contexts:
  default:
    stack: default
    cloud: grafana-com
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	def := cfg.Contexts["default"]
	assert.Equal(t, "plain-svc-token", def.Grafana.APIToken, "in-memory value should be plaintext")
	assert.Equal(t, "gat_plain", def.Grafana.OAuthToken)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	disk := string(raw)
	assert.Equal(t, 5, strings.Count(disk, "keychain:gcx:v2:"))
	assert.NotContains(t, disk, "plain-svc-token")
	assert.NotContains(t, disk, "gat_plain")

	storedBoundValue(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldGrafanaToken), "plain-svc-token")
	storedBoundValue(t, store, testCloudBinding(t, path, "grafana-com", credentials.FieldCloudToken), "plain-cloud-token")
}

func TestLoad_MigratesProviderSMToken(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
    providers:
      synth:
        sm-url: https://sm.example.invalid
        sm-token: plain-sm-token
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	def := cfg.Contexts["default"]
	assert.Equal(t, "plain-sm-token", def.Providers["synth"]["sm-token"], "in-memory value should be plaintext")
	assert.Equal(t, "https://sm.example.invalid", def.Providers["synth"]["sm-url"], "non-secret keys are untouched")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	disk := string(raw)
	assert.Contains(t, disk, "keychain:gcx:v2:")
	assert.NotContains(t, disk, "plain-sm-token")

	storedBoundValue(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldSMToken), "plain-sm-token")
}

func TestLoad_ResolvesSentinelsToPlaintext(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	tokenRef := storeBoundReference(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken), "gat_resolved")
	refreshRef := storeBoundReference(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthRefreshToken), "gar_resolved")
	contents := fmt.Sprintf(`version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: %s
      oauth-refresh-token: %s
contexts:
  default:
    stack: default
current-context: default
`, tokenRef.Sentinel, refreshRef.Sentinel)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	def := cfg.Contexts["default"]
	assert.Equal(t, "gat_resolved", def.Grafana.OAuthToken)
	assert.Equal(t, "gar_resolved", def.Grafana.OAuthRefreshToken)
}

func TestLoad_RejectsLegacySentinelsInVersionedConfig(t *testing.T) {
	store := withFakeStore(t)
	require.NoError(t, store.Set(credentials.AccountKey("default", credentials.FieldOAuthToken), "gat_legacy"))

	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: keychain:gcx:default:oauth-token
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Empty(t, cfg.Contexts["default"].Grafana.OAuthToken)
	got, err := store.Get(credentials.AccountKey("default", credentials.FieldOAuthToken))
	require.NoError(t, err)
	assert.Equal(t, "gat_legacy", got, "unbound versioned references must never select legacy accounts")
}

func TestLoad_IdempotentWithSentinels(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	ref := storeBoundReference(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken), "gat_resolved")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: %s
contexts:
  default:
    stack: default
current-context: default
`, ref.Sentinel), 0o600))
	seedSets := store.sets()

	_, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	firstStat, err := os.Stat(path)
	require.NoError(t, err)
	setsAfterFirstLoad := store.sets()

	_, err = config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	secondStat, err := os.Stat(path)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second))
	assert.Equal(t, 1, store.len())
	assert.Equal(t, seedSets, setsAfterFirstLoad, "first Load of a sentinel-backed config must not call store.Set")
	assert.Equal(t, setsAfterFirstLoad, store.sets(), "second Load must not re-migrate resolved sentinels into the store")
	assert.Equal(t, firstStat.ModTime(), secondStat.ModTime(), "second Load must not rewrite the config file")
}

func TestWrite_RoundTripsThroughSentinels(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	binding := testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken)
	oldRef := storeBoundReference(t, store, binding, "gat_old")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: %s
contexts:
  default:
    stack: default
current-context: default
`, oldRef.Sentinel), 0o600))

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	cfg.Contexts["default"].Grafana.OAuthToken = "gat_rotated"
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "keychain:gcx:v2:")
	assert.NotContains(t, string(raw), "gat_rotated")

	storedBoundValue(t, store, binding, "gat_rotated")
	_, err = store.Get(oldRef.Account)
	require.ErrorIs(t, err, credentials.ErrNotFound)

	assert.Equal(t, "gat_rotated", cfg.Contexts["default"].Grafana.OAuthToken)
}

func TestLoad_KeychainUnavailableKeepsPlaintext(t *testing.T) {
	// Default test store already returns ErrUnavailable for every operation,
	// so no explicit override is needed.
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: gat_plain
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Equal(t, "gat_plain", cfg.Contexts["default"].Grafana.OAuthToken)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "gat_plain", "plaintext should remain on disk when keychain is unavailable")
	assert.NotContains(t, string(raw), "keychain:", "no sentinel should be written when keychain is unavailable")
}

// Finding 1: a temporarily unavailable keychain must not cause an unrelated
// config write to permanently erase the sentinel reference from the YAML.
func TestWrite_PreservesSentinelWhenKeychainUnavailableAtLoad(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	binding := testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken)
	ref := storeBoundReference(t, store, binding, "gat_real")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: %s
contexts:
  default:
    stack: default
current-context: default
`, ref.Sentinel), 0o600))
	// Reads now fail as if the backend went away (locked session, missing DBus).
	store.setGetErr(credentials.ErrUnavailable)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	// The in-memory value is cleared so the command surfaces a missing
	// credential rather than sending the sentinel string as a token.
	assert.Empty(t, cfg.Contexts["default"].Grafana.OAuthToken)

	// An unrelated config write must round-trip the sentinel back to disk.
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), ref.Sentinel,
		"sentinel must survive a write while the keychain is unavailable")
	assert.False(t, store.deleted(ref.Account),
		"an unresolvable entry must not be deleted from the keychain")
}

// Finding 2: clearing a keychain-backed field (gcx config unset, or an
// auth-method switch that drops the old credential) must remove the stale
// keychain entry instead of orphaning it.
func TestWrite_UnsettingBackedFieldRemovesKeychainEntry(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	binding := testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken)
	ref := storeBoundReference(t, store, binding, "gat_old")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: %s
contexts:
  default:
    stack: default
current-context: default
`, ref.Sentinel), 0o600))

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	require.Equal(t, "gat_old", cfg.Contexts["default"].Grafana.OAuthToken)

	cfg.Contexts["default"].Grafana.OAuthToken = ""
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), cfg))

	_, err = store.Get(ref.Account)
	require.ErrorIs(t, err, credentials.ErrNotFound,
		"stale keychain entry must be deleted when its field is unset")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "keychain:gcx:v2:")
}

// Finding 3: secrets written by gcx login / gcx config set (no prior
// keychain-backed load) must be written through to the keychain, never left as
// plaintext on disk.
func TestWrite_NewPlaintextSecretIsWrittenThrough(t *testing.T) {
	store := withFakeStore(t)
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg := config.Config{
		CurrentContext: "default",
		Stacks: map[string]*config.StackConfig{
			"default": {
				Grafana: &config.GrafanaConfig{
					Server:   "https://example.invalid",
					APIToken: "plain-new-token",
				},
			},
		},
		Contexts: map[string]*config.Context{
			"default": {Stack: "default"},
		},
	}
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "keychain:gcx:v2:",
		"a freshly written plaintext secret must be replaced by a sentinel")
	assert.NotContains(t, string(raw), "plain-new-token")

	storedBoundValue(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldGrafanaToken), "plain-new-token")
}

func TestLoad_LazyResolvesOnlyCurrentContext(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.invalid
  staging:
    grafana:
      server: https://staging.invalid
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
current-context: prod
`)
	prodRef := storeBoundReference(t, store, testStackBinding(t, path, "prod", "https://prod.invalid", credentials.FieldOAuthToken), "gat_prod")
	stagingRef := storeBoundReference(t, store, testStackBinding(t, path, "staging", "https://staging.invalid", credentials.FieldOAuthToken), "gat_staging")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.invalid
      oauth-token: %s
  staging:
    grafana:
      server: https://staging.invalid
      oauth-token: %s
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
current-context: prod
`, prodRef.Sentinel, stagingRef.Sentinel), 0o600))

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Equal(t, "gat_prod", cfg.Contexts["prod"].Grafana.OAuthToken,
		"current context must be resolved eagerly during Load")
	assert.Equal(t, stagingRef.Sentinel, cfg.Contexts["staging"].Grafana.OAuthToken,
		"non-current context must keep its raw sentinel until resolved on demand")

	cfg.ResolveContext("staging")
	assert.Equal(t, "gat_staging", cfg.Contexts["staging"].Grafana.OAuthToken,
		"ResolveContext must resolve sentinels for the named context")
}

func TestLoad_OverrideSwitchingContextResolvesSentinels(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.invalid
  staging:
    grafana:
      server: https://staging.invalid
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
current-context: prod
`)
	ref := storeBoundReference(t, store, testStackBinding(t, path, "staging", "https://staging.invalid", credentials.FieldOAuthToken), "gat_staging")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.invalid
  staging:
    grafana:
      server: https://staging.invalid
      oauth-token: %s
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
current-context: prod
`, ref.Sentinel), 0o600))

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path), func(c *config.Config) error {
		c.CurrentContext = "staging"
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, "gat_staging", cfg.Contexts["staging"].Grafana.OAuthToken,
		"a context selected via override (e.g. --context) must have its sentinels resolved")
}

func TestResolveContext_NoOps(t *testing.T) {
	// A config built directly (never Loaded) has no keychain store: ResolveContext
	// must be a no-op and must not panic.
	direct := config.Config{
		Stacks: map[string]*config.StackConfig{
			"default": {Grafana: &config.GrafanaConfig{OAuthToken: "keychain:gcx:stack:default:oauth-token"}},
		},
		Contexts: map[string]*config.Context{
			"default": {Stack: "default"},
		},
	}
	direct.Resolve()
	direct.ResolveContext("default")
	assert.Equal(t, "keychain:gcx:stack:default:oauth-token", direct.Stacks["default"].Grafana.OAuthToken,
		"ResolveContext with no keychain store must leave the sentinel untouched")

	store := withFakeStore(t)
	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
contexts:
  default:
    stack: default
current-context: default
`)
	ref := storeBoundReference(t, store, testStackBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken), "gat_resolved")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, `version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: %s
contexts:
  default:
    stack: default
current-context: default
`, ref.Sentinel), 0o600))
	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	// Unknown context name: no-op, no panic.
	cfg.ResolveContext("does-not-exist")

	// Already-resolved current context: idempotent.
	cfg.ResolveContext("default")
	assert.Equal(t, "gat_resolved", cfg.Contexts["default"].Grafana.OAuthToken)
}

func TestLoadLayered_OverrideResolvesSentinelsForSelectedContext(t *testing.T) {
	store := withFakeStore(t)

	userDir, workDir := isolatedLoaderEnv(t)
	userPath := filepath.Join(userDir, "gcx", "config.yaml")
	writeLoaderConfig(t, userPath, `
version: 1
current-context: prod
stacks:
  prod:
    grafana:
      server: https://prod.invalid
  staging:
    grafana:
      server: https://staging.invalid
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
`)
	ref := storeBoundReference(t, store, testStackBinding(t, userPath, "staging", "https://staging.invalid", credentials.FieldOAuthToken), "gat_staging")
	writeLoaderConfig(t, userPath, fmt.Sprintf(`
version: 1
current-context: prod
stacks:
  prod:
    grafana:
      server: https://prod.invalid
  staging:
    grafana:
      server: https://staging.invalid
      oauth-token: %s
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
`, ref.Sentinel))
	// A local layer makes len(sources) >= 2, exercising the multi-source merge path
	// where each layer only resolves its own current-context.
	writeLoaderConfig(t, filepath.Join(workDir, config.LocalConfigFileName), `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod-local.invalid
`)

	cfg, err := config.LoadLayered(t.Context(), "", func(c *config.Config) error {
		c.CurrentContext = "staging"
		return nil
	})
	require.NoError(t, err)
	require.Len(t, cfg.Sources, 2, "test must exercise the multi-source merge path")

	assert.Equal(t, "gat_staging", cfg.Contexts["staging"].Grafana.OAuthToken,
		"--context selecting a context that was current in no layer must still resolve its sentinels")
}

func TestLoad_MalformedSentinelClearsField(t *testing.T) {
	withFakeStore(t)

	path := writeYAML(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: keychain:gcx:stack:wrong-stack:oauth-token
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Empty(t, cfg.Contexts["default"].Grafana.OAuthToken)
}
