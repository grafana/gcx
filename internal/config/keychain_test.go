package config_test

import (
	"os"
	"path/filepath"
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
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[string]string{}}
}

func (s *fakeStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return v, nil
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
	delete(s.entries, key)
	return nil
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

func writeYAML(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestLoad_MigratesPlaintextSecretsIntoKeychain(t *testing.T) {
	store := withFakeStore(t)
	path := writeYAML(t, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      token: plain-svc-token
      password: plain-password
      oauth-token: gat_plain
      oauth-refresh-token: gar_plain
    cloud:
      token: plain-cloud-token
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
	assert.Contains(t, disk, "keychain:gcx:default:cloud-token")
	assert.Contains(t, disk, "keychain:gcx:default:grafana-token")
	assert.Contains(t, disk, "keychain:gcx:default:grafana-password")
	assert.Contains(t, disk, "keychain:gcx:default:oauth-token")
	assert.Contains(t, disk, "keychain:gcx:default:oauth-refresh-token")
	assert.NotContains(t, disk, "plain-svc-token")
	assert.NotContains(t, disk, "gat_plain")

	got, err := store.Get(credentials.AccountKey("default", credentials.FieldGrafanaToken))
	require.NoError(t, err)
	assert.Equal(t, "plain-svc-token", got)
}

func TestLoad_ResolvesSentinelsToPlaintext(t *testing.T) {
	store := withFakeStore(t)
	require.NoError(t, store.Set(credentials.AccountKey("default", credentials.FieldOAuthToken), "gat_resolved"))
	require.NoError(t, store.Set(credentials.AccountKey("default", credentials.FieldOAuthRefreshToken), "gar_resolved"))

	path := writeYAML(t, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: keychain:gcx:default:oauth-token
      oauth-refresh-token: keychain:gcx:default:oauth-refresh-token
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	def := cfg.Contexts["default"]
	assert.Equal(t, "gat_resolved", def.Grafana.OAuthToken)
	assert.Equal(t, "gar_resolved", def.Grafana.OAuthRefreshToken)
}

func TestLoad_IdempotentWithSentinels(t *testing.T) {
	store := withFakeStore(t)
	require.NoError(t, store.Set(credentials.AccountKey("default", credentials.FieldOAuthToken), "gat_resolved"))
	seedSets := store.sets()

	path := writeYAML(t, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: keychain:gcx:default:oauth-token
current-context: default
`)

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
	require.NoError(t, store.Set(credentials.AccountKey("default", credentials.FieldOAuthToken), "gat_old"))

	path := writeYAML(t, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: keychain:gcx:default:oauth-token
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	cfg.Contexts["default"].Grafana.OAuthToken = "gat_rotated"
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "keychain:gcx:default:oauth-token")
	assert.NotContains(t, string(raw), "gat_rotated")

	got, err := store.Get(credentials.AccountKey("default", credentials.FieldOAuthToken))
	require.NoError(t, err)
	assert.Equal(t, "gat_rotated", got)

	assert.Equal(t, "gat_rotated", cfg.Contexts["default"].Grafana.OAuthToken)
}

func TestLoad_KeychainUnavailableKeepsPlaintext(t *testing.T) {
	// Default test store already returns ErrUnavailable for every operation,
	// so no explicit override is needed.
	path := writeYAML(t, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: gat_plain
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

func TestLoad_MalformedSentinelClearsField(t *testing.T) {
	withFakeStore(t)

	path := writeYAML(t, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      oauth-token: keychain:gcx:wrong-context:oauth-token
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Empty(t, cfg.Contexts["default"].Grafana.OAuthToken)
}
