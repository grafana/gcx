package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type boundTestStore struct {
	entries       map[string]string
	getErr        error
	setErr        error
	setErrValue   string
	setFailAt     int
	setCalls      int
	deleteErr     error
	deleteThenErr bool
	deleteFailAt  int
	deleteCalls   int
	gets          []string
	sets          []string
	deletes       []string
}

type boundTestLogger struct {
	warnings []string
}

func (*boundTestLogger) Debug(string, ...any) {}
func (*boundTestLogger) Info(string, ...any)  {}
func (l *boundTestLogger) Warn(message string, _ ...any) {
	l.warnings = append(l.warnings, message)
}
func (*boundTestLogger) Error(string, ...any) {}
func (l *boundTestLogger) With(...any) logging.Logger {
	return l
}
func (l *boundTestLogger) WithContext(context.Context) logging.Logger {
	return l
}

func newBoundTestStore() *boundTestStore {
	return &boundTestStore{entries: map[string]string{}}
}

func (s *boundTestStore) Get(key string) (string, error) {
	s.gets = append(s.gets, key)
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.entries[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return value, nil
}

func (s *boundTestStore) Set(key, value string) error {
	s.setCalls++
	valueMatches := s.setErrValue != "" && value == s.setErrValue
	callMatches := s.setErrValue == "" && (s.setFailAt == 0 || s.setCalls == s.setFailAt)
	if s.setErr != nil && (valueMatches || callMatches) {
		return s.setErr
	}
	s.sets = append(s.sets, key)
	s.entries[key] = value
	return nil
}

func (s *boundTestStore) Delete(key string) error {
	s.deleteCalls++
	shouldFail := s.deleteErr != nil && (s.deleteFailAt == 0 || s.deleteCalls == s.deleteFailAt)
	if shouldFail && s.deleteThenErr {
		s.deletes = append(s.deletes, key)
		delete(s.entries, key)
		return s.deleteErr
	}
	if shouldFail {
		return s.deleteErr
	}
	s.deletes = append(s.deletes, key)
	delete(s.entries, key)
	return nil
}

func TestBoundKeychainIncompleteCleanupRollbackRetainsCommittedGenerations(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://example.invalid", "old-token")
	cfg.Stacks["default"].Grafana.User = "user"
	cfg.Stacks["default"].Grafana.Password = "old-password"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	original, err := os.ReadFile(path)
	require.NoError(t, err)

	loaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	loaded.Stacks["default"].Grafana.APIToken = "new-token"
	loaded.Stacks["default"].Grafana.Password = "new-password"

	// Two new generations are staged first. The second old-generation delete
	// then reports failure after deleting its account, and the first attempted
	// restore also fails. Reverting the YAML would therefore strand it.
	store.setCalls = 0
	store.setErr = errors.New("injected restore failure")
	store.setFailAt = 3
	store.deleteCalls = 0
	store.deleteErr = errors.New("injected delete failure")
	store.deleteFailAt = 2
	store.deleteThenErr = true
	err = Write(context.Background(), ExplicitConfigFile(path), loaded)
	require.ErrorContains(t, err, "retained the committed config and new credential generations")

	committed, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.NotEqual(t, original, committed)

	store.setErr = nil
	store.deleteErr = nil
	reloaded, loadErr := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, loadErr)
	assert.Equal(t, "new-token", reloaded.Stacks["default"].Grafana.APIToken)
	assert.Equal(t, "new-password", reloaded.Stacks["default"].Grafana.Password)
}

func TestBoundKeychainDeleteFailureRestoresConfigAndGeneration(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	account := storedBoundAccount(t, store, binding, "token")
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.APIToken = ""
	store.deleteErr = credentials.ErrUnavailable
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorIs(t, err, credentials.ErrUnavailable)

	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Equal(t, "token", store.entries[account])
}

func TestBoundKeychainDeleteThenErrorRestoresConfigAndGeneration(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	account := storedBoundAccount(t, store, binding, "token")
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.APIToken = ""
	store.deleteErr = errors.New("backend deleted entry but lost acknowledgement")
	store.deleteThenErr = true
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "lost acknowledgement")

	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Equal(t, "token", store.entries[account])
}

func useBoundTestStore(t *testing.T, store *boundTestStore) {
	t.Helper()
	original := keychainStoreFn
	keychainStoreFn = func() credentials.Store { return store }
	t.Cleanup(func() { keychainStoreFn = original })
}

func storedBoundAccount(t *testing.T, store *boundTestStore, binding credentials.Binding, value string) string {
	t.Helper()
	var matches []string
	for account, stored := range store.entries {
		if credentials.MatchesBoundAccount(account, binding) && (value == "" || stored == value) {
			matches = append(matches, account)
		}
	}
	require.Len(t, matches, 1)
	return matches[0]
}

func assertNoStoredBoundAccount(t *testing.T, store *boundTestStore, binding credentials.Binding) {
	t.Helper()
	for account := range store.entries {
		assert.False(t, credentials.MatchesBoundAccount(account, binding), "unexpected generated account %s", account)
	}
}

func assertBoundAccountCount(t *testing.T, store *boundTestStore, binding credentials.Binding, count int) {
	t.Helper()
	actual := 0
	for account := range store.entries {
		if credentials.MatchesBoundAccount(account, binding) {
			actual++
		}
	}
	assert.Equal(t, count, actual)
}

func boundStackTestConfig(server, token string) Config {
	return Config{
		Version:        ConfigVersion,
		CurrentContext: "default",
		Stacks: map[string]*StackConfig{
			"default": {Grafana: &GrafanaConfig{Server: server, APIToken: token}},
		},
		Contexts: map[string]*Context{"default": {Stack: "default"}},
	}
}

func TestGrafanaTokenBindingMatchesCompleteAuthorityBoundary(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	server := "https://example.invalid"
	pathA := filepath.Join(t.TempDir(), "a.yaml")
	pathB := filepath.Join(t.TempDir(), "b.yaml")
	require.NoError(t, Write(t.Context(), ExplicitConfigFile(pathA), boundStackTestConfig(server, "token")))
	require.NoError(t, Write(t.Context(), ExplicitConfigFile(pathB), boundStackTestConfig(server, "token")))

	stored, err := Load(t.Context(), ExplicitConfigFile(pathA))
	require.NoError(t, err)
	effective, err := Load(t.Context(), ExplicitConfigFile(pathA))
	require.NoError(t, err)
	assert.True(t, GrafanaTokenBindingMatches(
		stored.Contexts["default"],
		effective.Contexts["default"],
		server,
	))

	otherSource, err := Load(t.Context(), ExplicitConfigFile(pathB))
	require.NoError(t, err)
	assert.False(t, GrafanaTokenBindingMatches(
		stored.Contexts["default"],
		otherSource.Contexts["default"],
		server,
	), "an identical destination in another config file is a different credential authority")

	effective.Contexts["default"].Grafana.ProxyEndpoint = "https://proxy.example.invalid"
	assert.False(t, GrafanaTokenBindingMatches(
		stored.Contexts["default"],
		effective.Contexts["default"],
		server,
	), "proxy and TLS components must participate in the complete binding")
}

func boundStackTestBinding(t *testing.T, path, name, server string, field credentials.Field) credentials.Binding {
	t.Helper()
	source, err := canonicalConfigSource(path)
	require.NoError(t, err)
	stack := &StackConfig{sourceIdentity: source, Grafana: &GrafanaConfig{Server: server}}
	return stackOwner(name, stack).binding(field)
}

func writeBoundTestYAML(t *testing.T, path, server, fieldName, sentinel string) {
	t.Helper()
	contents := fmt.Sprintf(`version: 1
stacks:
  default:
    grafana:
      server: %s
      %s: %s
contexts:
  default:
    stack: default
current-context: default
`, server, fieldName, sentinel)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func TestBoundKeychainSameNamedEntriesAreIsolatedBySource(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	server := "https://example.invalid"
	pathA := filepath.Join(t.TempDir(), "config.yaml")
	pathB := filepath.Join(t.TempDir(), "config.yaml")

	require.NoError(t, Write(context.Background(), ExplicitConfigFile(pathA), boundStackTestConfig(server, "token-a")))
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(pathB), boundStackTestConfig(server, "token-b")))

	bindingA := boundStackTestBinding(t, pathA, "default", server, credentials.FieldGrafanaToken)
	bindingB := boundStackTestBinding(t, pathB, "default", server, credentials.FieldGrafanaToken)
	accountA := storedBoundAccount(t, store, bindingA, "token-a")
	accountB := storedBoundAccount(t, store, bindingB, "token-b")
	require.NotEqual(t, accountA, accountB)
	assert.Equal(t, "token-a", store.entries[accountA])
	assert.Equal(t, "token-b", store.entries[accountB])

	loadedA, err := Load(context.Background(), ExplicitConfigFile(pathA))
	require.NoError(t, err)
	loadedB, err := Load(context.Background(), ExplicitConfigFile(pathB))
	require.NoError(t, err)
	assert.Equal(t, "token-a", loadedA.Stacks["default"].Grafana.APIToken)
	assert.Equal(t, "token-b", loadedB.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainRejectsForeignCrossFieldAndLegacyReferencesWithoutGet(t *testing.T) {
	server := "https://attacker.invalid"
	tests := []struct {
		name      string
		sentinel  func(path string) (credentials.Binding, string)
		fieldName string
	}{
		{
			name: "foreign source and destination",
			sentinel: func(string) (credentials.Binding, string) {
				victimPath := filepath.Join(t.TempDir(), "config.yaml")
				binding := boundStackTestBinding(t, victimPath, "default", "https://victim.invalid", credentials.FieldGrafanaToken)
				return binding, credentials.FormatBoundSentinel(binding)
			},
			fieldName: "token",
		},
		{
			name: "cross field",
			sentinel: func(path string) (credentials.Binding, string) {
				binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
				return binding, credentials.FormatBoundSentinel(binding)
			},
			fieldName: "password",
		},
		{
			name: "legacy unbound",
			sentinel: func(string) (credentials.Binding, string) {
				return credentials.Binding{}, credentials.FormatSentinel("default", credentials.FieldGrafanaToken)
			},
			fieldName: "token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newBoundTestStore()
			useBoundTestStore(t, store)
			path := filepath.Join(t.TempDir(), "config.yaml")
			binding, sentinel := tt.sentinel(path)
			if binding.Valid() {
				store.entries[credentials.BoundAccountKey(binding)] = "victim-secret"
			} else {
				store.entries[credentials.AccountKey("default", credentials.FieldGrafanaToken)] = "victim-secret"
			}
			writeBoundTestYAML(t, path, server, tt.fieldName, sentinel)

			cfg, err := Load(context.Background(), ExplicitConfigFile(path))
			require.NoError(t, err)
			assert.Empty(t, store.gets, "a config-file reference must not select a foreign keychain account")
			if tt.fieldName == "password" {
				assert.Empty(t, cfg.Stacks["default"].Grafana.Password)
			} else {
				assert.Empty(t, cfg.Stacks["default"].Grafana.APIToken)
			}
			_, restErr := cfg.Contexts["default"].ToRESTConfig(context.Background())
			require.Error(t, restErr)
			var rejected CredentialRejectedError
			require.ErrorAs(t, restErr, &rejected)
			assert.Contains(t, restErr.Error(), "before network use")
		})
	}
}

func TestMovedThreeContextConfigPreservesAndRepairsForeignSentinelsIncrementally(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	oldPath := filepath.Join(t.TempDir(), "config.yaml")
	movedPath := filepath.Join(t.TempDir(), "config.yaml")
	names := []string{"prod", "staging", "dev"}
	servers := map[string]string{
		"prod":    "https://prod.invalid",
		"staging": "https://staging.invalid",
		"dev":     "https://dev.invalid",
	}
	sentinels := map[string]string{}
	oldAccounts := map[string]string{}
	for _, name := range names {
		binding := boundStackTestBinding(t, oldPath, name, servers[name], credentials.FieldGrafanaToken)
		sentinels[name] = credentials.FormatBoundSentinel(binding)
		oldAccounts[name] = credentials.BoundAccountKey(binding)
		store.entries[oldAccounts[name]] = "old-" + name
	}
	contents := fmt.Sprintf(`version: 1
stacks:
  prod:
    grafana:
      server: %s
      token: %s
      auth-method: token
  staging:
    grafana:
      server: %s
      token: %s
      auth-method: token
  dev:
    grafana:
      server: %s
      token: %s
      auth-method: token
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
  dev:
    stack: dev
current-context: prod
`, servers["prod"], sentinels["prod"], servers["staging"], sentinels["staging"], servers["dev"], sentinels["dev"])
	require.NoError(t, os.WriteFile(movedPath, []byte(contents), 0o600))

	cfg, err := Load(context.Background(), ExplicitConfigFile(movedPath))
	require.NoError(t, err)
	assert.Empty(t, store.gets, "a moved config must not retrieve any foreign generation")
	for _, name := range names {
		cfg.ResolveContext(name)
		assert.Empty(t, cfg.Stacks[name].Grafana.APIToken)
	}

	// An unrelated write must remain possible and preserve every foreign
	// sentinel exactly, even when none of the three owners has been repaired.
	cfg.Resources = &ResourcesConfig{AssumeServerDryRun: []string{"folders.folder.grafana.app"}}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(movedPath), cfg))
	raw, err := os.ReadFile(movedPath)
	require.NoError(t, err)
	for _, sentinel := range sentinels {
		assert.Contains(t, string(raw), sentinel)
	}

	// Re-authenticate owners in a non-current order. Each write replaces only
	// that owner's rejected reference and leaves every other owner repairable.
	for _, name := range []string{"dev", "prod", "staging"} {
		cfg, err = Load(context.Background(), ExplicitConfigFile(movedPath))
		require.NoError(t, err)
		cfg.ResolveContext(name)
		cfg.Stacks[name].Grafana.APIToken = "fresh-" + name
		cfg.MarkSecretMutation(credentials.StackOwner(name), credentials.FieldGrafanaToken)
		require.NoError(t, Write(context.Background(), ExplicitConfigFile(movedPath), cfg))
	}

	final, err := Load(context.Background(), ExplicitConfigFile(movedPath))
	require.NoError(t, err)
	for _, name := range names {
		final.ResolveContext(name)
		assert.Equal(t, "fresh-"+name, final.Stacks[name].Grafana.APIToken)
		assert.NotContains(t, store.gets, oldAccounts[name], "foreign generations must never be read")
		assert.Equal(t, "old-"+name, store.entries[oldAccounts[name]], "foreign generations must never be deleted")
	}
}

func TestBoundKeychainDeferredResolutionUsesEntryProvenance(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	prodBinding := boundStackTestBinding(t, path, "prod", "https://prod.invalid", credentials.FieldOAuthToken)
	stagingBinding := boundStackTestBinding(t, path, "staging", "https://staging.invalid", credentials.FieldOAuthToken)
	store.entries[credentials.BoundAccountKey(prodBinding)] = "prod-token"
	store.entries[credentials.BoundAccountKey(stagingBinding)] = "staging-token"
	contents := fmt.Sprintf(`version: 1
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
`, credentials.FormatBoundSentinel(prodBinding), credentials.FormatBoundSentinel(stagingBinding))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, []string{credentials.BoundAccountKey(prodBinding)}, store.gets)
	assert.Equal(t, "prod-token", cfg.Stacks["prod"].Grafana.OAuthToken)
	assert.Equal(t, credentials.FormatBoundSentinel(stagingBinding), cfg.Stacks["staging"].Grafana.OAuthToken)

	cfg.ResolveContext("staging")
	assert.Equal(t, "staging-token", cfg.Stacks["staging"].Grafana.OAuthToken)
	assert.Equal(t, []string{
		credentials.BoundAccountKey(prodBinding),
		credentials.BoundAccountKey(stagingBinding),
	}, store.gets)
}

func TestBoundKeychainUnavailableReferenceIsPreservedUntilExplicitUnset(t *testing.T) {
	store := newBoundTestStore()
	store.getErr = credentials.ErrUnavailable
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	binding := boundStackTestBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken)
	account := credentials.BoundAccountKey(binding)
	store.entries[account] = "real-token"
	sentinel := credentials.FormatBoundSentinel(binding)
	writeBoundTestYAML(t, path, "https://example.invalid", "oauth-token", sentinel)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Empty(t, cfg.Stacks["default"].Grafana.OAuthToken)
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), sentinel)
	assert.Empty(t, store.deletes)

	require.True(t, cfg.MarkSecretPathMutation("stacks.default.grafana.oauth-token"))
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "cannot verify keychain deletion")
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), sentinel)
	assert.NotContains(t, store.deletes, account)

	store.getErr = nil
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "keychain:")
	assert.Contains(t, store.deletes, account)
}

func TestBoundKeychainWholeOwnerDeletionRemovesExactAccount(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	account := storedBoundAccount(t, store, binding, "token")

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	delete(cfg.Stacks, "default")
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	assert.Contains(t, store.deletes, account)
	_, present := store.entries[account]
	assert.False(t, present)
}

func TestBoundKeychainDestinationChangeRequiresCredentialRotation(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	oldServer := "https://old.invalid"
	newServer := "https://new.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(oldServer, "old-token")))
	oldBinding := boundStackTestBinding(t, path, "default", oldServer, credentials.FieldGrafanaToken)
	oldAccount := storedBoundAccount(t, store, oldBinding, "old-token")

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)
	setsBefore := len(store.sets)
	cfg.Stacks["default"].Grafana.Server = newServer
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "credential destination changed")
	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Len(t, store.sets, setsBefore)
	assert.NotContains(t, store.deletes, oldAccount)

	cfg.Stacks["default"].Grafana.APIToken = "new-token"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	newBinding := boundStackTestBinding(t, path, "default", newServer, credentials.FieldGrafanaToken)
	newAccount := storedBoundAccount(t, store, newBinding, "new-token")
	assert.Equal(t, "new-token", store.entries[newAccount])
	assert.Contains(t, store.deletes, oldAccount)
}

func TestGrafanaServerMutationInvalidatesBoundSMToken(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://old-grafana.invalid", "")
	cfg.Stacks["default"].Providers = map[string]map[string]string{
		"synth": {"sm-url": "https://sm.invalid", "sm-token": "sm-token"},
	}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	loaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	finish := loaded.PrepareSecretPathMutation("stacks.default.grafana.server")
	loaded.Stacks["default"].Grafana.Server = "https://new-grafana.invalid"
	require.NoError(t, finish())
	assert.Empty(t, loaded.Stacks["default"].Providers["synth"]["sm-token"])
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), loaded))
}

func TestGrafanaTLSObjectUnsetClearsPlaintextCredentialsWhenKeychainUnavailable(t *testing.T) {
	store := newBoundTestStore()
	store.getErr = credentials.ErrUnavailable
	store.setErr = credentials.ErrUnavailable
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: secret-grafana-api
      user: alice
      password: secret-grafana-password
      oauth-token: secret-oauth-access
      oauth-refresh-token: secret-oauth-refresh
      tls:
        ca-data: dHJ1c3RlZC1jYQ==
    providers:
      synth:
        sm-url: https://sm.invalid
        sm-token: secret-synth
contexts:
  default:
    stack: default
current-context: default
`), 0o600))

	loaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	stack := loaded.Stacks["default"]
	require.NotNil(t, stack)
	require.NotNil(t, stack.Grafana)
	assert.Equal(t, "secret-grafana-api", stack.Grafana.APIToken)
	assert.Equal(t, "secret-grafana-password", stack.Grafana.Password)
	assert.Equal(t, "secret-oauth-access", stack.Grafana.OAuthToken)
	assert.Equal(t, "secret-oauth-refresh", stack.Grafana.OAuthRefreshToken)
	assert.Equal(t, "secret-synth", stack.Providers["synth"]["sm-token"])

	finish := loaded.PrepareSecretPathMutation("stacks.default.grafana.tls")
	stack.Grafana.TLS = nil
	require.NoError(t, finish())
	assert.Empty(t, stack.Grafana.APIToken)
	assert.Empty(t, stack.Grafana.Password)
	assert.Empty(t, stack.Grafana.OAuthToken)
	assert.Empty(t, stack.Grafana.OAuthRefreshToken)
	assert.Empty(t, stack.Providers["synth"]["sm-token"])
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), loaded))

	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	for _, secret := range []string{"secret-grafana-api", "secret-grafana-password", "secret-oauth-access", "secret-oauth-refresh", "secret-synth"} {
		assert.NotContains(t, string(raw), secret)
	}
}

func TestBoundKeychainSourceChangeRequiresReauthentication(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	pathA := filepath.Join(t.TempDir(), "config.yaml")
	pathB := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(pathA), boundStackTestConfig("https://example.invalid", "token")))
	cfg, err := Load(context.Background(), ExplicitConfigFile(pathA))
	require.NoError(t, err)

	err = Write(context.Background(), ExplicitConfigFile(pathB), cfg)
	require.ErrorContains(t, err, "refusing to write credential owner")
	_, statErr := os.Stat(pathB)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestMergeKeychainRuntimeDropsShadowedSameNamedState(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	basePath := filepath.Join(t.TempDir(), "config.yaml")
	overPath := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(basePath), boundStackTestConfig(server, "base-token")))
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(overPath), boundStackTestConfig(server, "over-token")))
	baseBinding := boundStackTestBinding(t, basePath, "default", server, credentials.FieldGrafanaToken)
	overBinding := boundStackTestBinding(t, overPath, "default", server, credentials.FieldGrafanaToken)
	baseAccount := storedBoundAccount(t, store, baseBinding, "base-token")
	overAccount := storedBoundAccount(t, store, overBinding, "over-token")

	base, err := Load(context.Background(), ExplicitConfigFile(basePath))
	require.NoError(t, err)
	over, err := Load(context.Background(), ExplicitConfigFile(overPath))
	require.NoError(t, err)
	merged := MergeConfigs(base, over)
	assert.Equal(t, "over-token", merged.Stacks["default"].Grafana.APIToken)
	merged.Stacks["default"].Grafana.APIToken = ""
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(overPath), merged))

	assert.Equal(t, "base-token", store.entries[baseAccount])
	_, overPresent := store.entries[overAccount]
	assert.False(t, overPresent)
	assert.NotContains(t, store.deletes, baseAccount)
}

func TestBoundKeychainMissingReferenceSurvivesUnrelatedWriteWithoutDelete(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	binding := boundStackTestBinding(t, path, "default", "https://example.invalid", credentials.FieldOAuthToken)
	sentinel := credentials.FormatBoundSentinel(binding)
	writeBoundTestYAML(t, path, "https://example.invalid", "oauth-token", sentinel)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Empty(t, cfg.Stacks["default"].Grafana.OAuthToken)
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), sentinel,
		"an unrelated write must preserve evidence that authentication was configured")
	assert.Empty(t, store.deletes, "a proven-missing account must not trigger an unrelated delete")

	reloaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	_, err = reloaded.Contexts["default"].ToRESTConfig(context.Background())
	require.Error(t, err)
	var rejected CredentialRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, credentials.FieldOAuthToken, rejected.Field)
}

func TestBoundKeychainRenameFailureRollsBackSameAccountRotation(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "old-token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	account := storedBoundAccount(t, store, binding, "old-token")
	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.APIToken = "new-token"

	originalRename := renameConfigFile
	renameConfigFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameConfigFile = originalRename })
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "injected rename failure")

	assert.Equal(t, "old-token", store.entries[account])
	assertBoundAccountCount(t, store, binding, 1)
	for candidate, value := range store.entries {
		if credentials.MatchesBoundAccount(candidate, binding) {
			assert.NotEqual(t, "new-token", value)
		}
	}
	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Equal(t, "new-token", cfg.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainRenameFailureRollsBackDestinationRotation(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	oldServer := "https://old.invalid"
	newServer := "https://new.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(oldServer, "old-token")))
	oldBinding := boundStackTestBinding(t, path, "default", oldServer, credentials.FieldGrafanaToken)
	oldAccount := storedBoundAccount(t, store, oldBinding, "old-token")
	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.Server = newServer
	cfg.Stacks["default"].Grafana.APIToken = "new-token"
	newBinding := boundStackTestBinding(t, path, "default", newServer, credentials.FieldGrafanaToken)

	originalRename := renameConfigFile
	renameConfigFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameConfigFile = originalRename })
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "injected rename failure")

	assert.Equal(t, "old-token", store.entries[oldAccount])
	assertNoStoredBoundAccount(t, store, newBinding)
	assert.NotContains(t, store.deletes, oldAccount)
}

func TestBoundKeychainRenameFailureKeepsDeletedOwnerAccount(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	account := storedBoundAccount(t, store, binding, "token")
	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	delete(cfg.Stacks, "default")

	originalRename := renameConfigFile
	renameConfigFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameConfigFile = originalRename })
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "injected rename failure")

	assert.Equal(t, "token", store.entries[account])
	assert.NotContains(t, store.deletes, account)
}

func TestBoundKeychainSymlinkRoundTripPreservesLinkAndBinding(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	target := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("version: 1\ncontexts: {}\ncurrent-context: \"\"\n"), 0o600))
	link := filepath.Join(t.TempDir(), "config-link.yaml")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, Write(context.Background(), ExplicitConfigFile(link), boundStackTestConfig("https://example.invalid", "token")))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)

	cfg, err := Load(context.Background(), ExplicitConfigFile(link))
	require.NoError(t, err)
	assert.Equal(t, "token", cfg.Stacks["default"].Grafana.APIToken)
	info, err = os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
}

func TestExplicitCurrentDirectoryLocalSymlinkRemainsUserAuthorized(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	dir := t.TempDir()
	t.Chdir(dir)
	target := filepath.Join(dir, "target.yaml")
	require.NoError(t, os.WriteFile(target, []byte("version: 1\ncontexts: {}\ncurrent-context: \"\"\n"), 0o600))
	link := filepath.Join(dir, LocalConfigFileName)
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, Write(context.Background(), ExplicitConfigFile(link), boundStackTestConfig("https://example.invalid", "token")))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
	loaded, err := Load(context.Background(), ExplicitConfigFile(link))
	require.NoError(t, err)
	assert.Equal(t, "token", loaded.Stacks["default"].Grafana.APIToken)
}

func TestAutoDiscoveredLocalSymlinkIsRejectedWithoutSideEffects(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "victim.yaml")
	original := []byte("contexts:\n  default:\n    grafana:\n      server: https://victim.invalid\n      token: victim-token\ncurrent-context: default\n")
	require.NoError(t, os.WriteFile(target, original, 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, LocalConfigFileName)))

	_, err := DiscoverSources(WithSystemDir(filepath.Join(dir, "no-system")), WithUserDir(filepath.Join(dir, "no-user")), WithWorkDir(dir))
	require.ErrorContains(t, err, "symlinks are not allowed")
	after, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, original, after)
	assert.Empty(t, store.gets)
	assert.Empty(t, store.sets)
	assert.Empty(t, store.deletes)
}

func TestBoundKeychainGrafanaDestinationOverrideClearsStoredCredential(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig("https://original.invalid", "stored-token")))

	t.Run("destination only", func(t *testing.T) {
		t.Setenv("GRAFANA_SERVER", "https://override.invalid")
		cfg, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
			return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
		})
		require.NoError(t, err)
		assert.Equal(t, "https://override.invalid", cfg.Contexts["default"].Grafana.Server)
		assert.Empty(t, cfg.Contexts["default"].Grafana.APIToken)
	})

	t.Run("explicit env credential", func(t *testing.T) {
		t.Setenv("GRAFANA_SERVER", "https://override.invalid")
		t.Setenv("GRAFANA_TOKEN", "stored-token")
		cfg, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
			return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
		})
		require.NoError(t, err)
		assert.Equal(t, "stored-token", cfg.Contexts["default"].Grafana.APIToken)
	})
}

func TestBoundKeychainCloudDestinationOverrideClearsCAPAndOAuth(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Config{
		Version:        ConfigVersion,
		CurrentContext: "default",
		Cloud: map[string]*CloudEntry{
			"grafana-com": {Token: "stored-cap", OAuthToken: "stored-oauth"},
		},
		Contexts: map[string]*Context{"default": {Cloud: "grafana-com"}},
	}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	t.Run("destination only", func(t *testing.T) {
		t.Setenv("GRAFANA_CLOUD_API_URL", "https://override.invalid")
		loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
			return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
		})
		require.NoError(t, err)
		assert.Equal(t, "https://override.invalid", loaded.Contexts["default"].CloudEntry.APIUrl)
		assert.Empty(t, loaded.Contexts["default"].CloudEntry.Token)
		assert.Empty(t, loaded.Contexts["default"].CloudEntry.OAuthToken)
	})

	t.Run("explicit env CAP", func(t *testing.T) {
		t.Setenv("GRAFANA_CLOUD_API_URL", "https://override.invalid")
		t.Setenv("GRAFANA_CLOUD_TOKEN", "stored-cap")
		loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
			return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
		})
		require.NoError(t, err)
		assert.Equal(t, "stored-cap", loaded.Contexts["default"].CloudEntry.Token)
		assert.Empty(t, loaded.Contexts["default"].CloudEntry.OAuthToken)
	})
}

func TestBoundKeychainSynthDestinationOverrideClearsStoredCredential(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://grafana.invalid", "")
	cfg.Stacks["default"].Providers = map[string]map[string]string{
		"synth": {"sm-url": "https://sm-original.invalid", "sm-token": "stored-sm-token"},
	}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
		cfg.Contexts["default"].Providers["synth"]["sm-url"] = "https://sm-override.invalid"
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, loaded.Contexts["default"].Providers["synth"]["sm-token"])

	explicit, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
		cfg.Contexts["default"].Providers["synth"]["sm-url"] = "https://sm-override.invalid"
		cfg.Contexts["default"].Providers["synth"]["sm-token"] = "explicit-sm-token"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "explicit-sm-token", explicit.Contexts["default"].Providers["synth"]["sm-token"])
}

func TestPlaintextCredentialsCannotBeRedirectedWhenKeychainUnavailable(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		override   Override
		credential func(Config) string
	}{
		{
			name: "grafana",
			yaml: `version: 1
stacks:
  default:
    grafana:
      server: https://original.invalid
      token: plaintext-token
contexts:
  default:
    stack: default
current-context: default
`,
			override: func(cfg *Config) error {
				cfg.Contexts["default"].Grafana.Server = "https://override.invalid"
				return nil
			},
			credential: func(cfg Config) string { return cfg.Contexts["default"].Grafana.APIToken },
		},
		{
			name: "cloud",
			yaml: `version: 1
cloud:
  grafana-com:
    token: plaintext-cap
    oauth-token: plaintext-oauth
contexts:
  default:
    cloud: grafana-com
current-context: default
`,
			override: func(cfg *Config) error {
				detached := *cfg.Contexts["default"].CloudEntry
				detached.APIUrl = "https://override.invalid"
				cfg.Contexts["default"].CloudEntry = &detached
				return nil
			},
			credential: func(cfg Config) string {
				return cfg.Contexts["default"].CloudEntry.Token + cfg.Contexts["default"].CloudEntry.OAuthToken
			},
		},
		{
			name: "synth",
			yaml: `version: 1
stacks:
  default:
    grafana:
      server: https://grafana.invalid
    providers:
      synth:
        sm-url: https://sm-original.invalid
        sm-token: plaintext-sm-token
contexts:
  default:
    stack: default
current-context: default
`,
			override: func(cfg *Config) error {
				cfg.Contexts["default"].Providers["synth"]["sm-url"] = "https://sm-override.invalid"
				return nil
			},
			credential: func(cfg Config) string { return cfg.Contexts["default"].Providers["synth"]["sm-token"] },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newBoundTestStore()
			store.setErr = credentials.ErrUnavailable
			useBoundTestStore(t, store)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))

			cfg, err := Load(context.Background(), ExplicitConfigFile(path), tt.override)
			require.NoError(t, err)
			assert.Empty(t, tt.credential(cfg))
		})
	}
}

func TestRuntimeExplicitCredentialOnSharedStackIsNotClearedByOtherContext(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://original.invalid", "stored-token")
	cfg.Contexts["other"] = &Context{Stack: "default"}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	t.Setenv("GRAFANA_SERVER", "https://override.invalid")
	t.Setenv("GRAFANA_TOKEN", "stored-token")

	loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
		return ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
	})
	require.NoError(t, err)
	assert.Equal(t, "stored-token", loaded.Contexts["default"].Grafana.APIToken)
	assert.Equal(t, "stored-token", loaded.Contexts["other"].Grafana.APIToken)
}

func TestBoundKeychainGenericStoreFailuresLeaveDiskAndCallerUntouched(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*boundTestStore)
	}{
		{
			name: "get",
			setup: func(store *boundTestStore) {
				store.getErr = errors.New("generic get failure")
			},
		},
		{
			name: "second set",
			setup: func(store *boundTestStore) {
				store.setErr = errors.New("generic set failure")
				store.setFailAt = 2
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newBoundTestStore()
			tt.setup(store)
			useBoundTestStore(t, store)
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := []byte("version: 1\ncontexts: {}\ncurrent-context: \"\"\n")
			require.NoError(t, os.WriteFile(path, original, 0o600))
			cfg := boundStackTestConfig("https://example.invalid", "api-token")
			cfg.Stacks["default"].Grafana.User = "alice"
			cfg.Stacks["default"].Grafana.Password = "password"

			err := Write(context.Background(), ExplicitConfigFile(path), cfg)
			require.Error(t, err)
			raw, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, original, raw)
			assert.Empty(t, store.entries, "partially staged generations must be rolled back")
			assert.Equal(t, "api-token", cfg.Stacks["default"].Grafana.APIToken)
			assert.Equal(t, "password", cfg.Stacks["default"].Grafana.Password)
			assert.Empty(t, cfg.Stacks["default"].sourceIdentity)
		})
	}
}

func TestBoundKeychainWriteUnavailableFallsBackOnlyForNewCredential(t *testing.T) {
	store := newBoundTestStore()
	store.setErr = credentials.ErrUnavailable
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://example.invalid", "new-token")

	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "new-token")
	assert.NotContains(t, string(raw), "keychain:gcx:v2:")
	assert.Empty(t, store.entries)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestBoundKeychainWriteUnavailableMissingReferenceRepairFailsClosed(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	sentinel := credentials.FormatBoundSentinel(binding)
	writeBoundTestYAML(t, path, server, "token", sentinel)
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Empty(t, cfg.Stacks["default"].Grafana.APIToken)
	cfg.Stacks["default"].Grafana.APIToken = "replacement-token"
	store.setErr = credentials.ErrUnavailable

	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorIs(t, err, credentials.ErrUnavailable)
	require.ErrorContains(t, err, "cannot replace credential")
	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Contains(t, string(rawAfter), sentinel)
	assert.Empty(t, store.entries)
	assert.Empty(t, store.deletes)
	assert.Equal(t, "replacement-token", cfg.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainWriteUnavailableRejectedReferenceRepairFailsClosed(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	foreignPath := filepath.Join(t.TempDir(), "foreign-config.yaml")
	foreignBinding := boundStackTestBinding(t, foreignPath, "default", server, credentials.FieldGrafanaToken)
	foreignAccount := credentials.BoundAccountKey(foreignBinding)
	foreignSentinel := credentials.FormatBoundSentinel(foreignBinding)
	store.entries[foreignAccount] = "foreign-token"
	writeBoundTestYAML(t, path, server, "token", foreignSentinel)
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Empty(t, cfg.Stacks["default"].Grafana.APIToken)
	cfg.Stacks["default"].Grafana.APIToken = "replacement-token"
	store.setErr = credentials.ErrUnavailable

	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorIs(t, err, credentials.ErrUnavailable)
	require.ErrorContains(t, err, "cannot replace credential")
	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Contains(t, string(rawAfter), foreignSentinel)
	assert.Equal(t, map[string]string{foreignAccount: "foreign-token"}, store.entries)
	assert.Empty(t, store.deletes)
	assert.Equal(t, "replacement-token", cfg.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainPartialWriteUnavailableCommitsSentinelAndPlaintextSafely(t *testing.T) {
	store := newBoundTestStore()
	store.setErr = credentials.ErrUnavailable
	store.setFailAt = 2
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://example.invalid", "new-token")
	cfg.Stacks["default"].Grafana.User = "alice"
	cfg.Stacks["default"].Grafana.Password = "new-password"

	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	disk := string(raw)
	assert.Len(t, store.entries, 1)
	assert.NotEqual(t, strings.Contains(disk, "new-token"), strings.Contains(disk, "new-password"),
		"exactly one credential should use the plaintext fallback")
	assert.Equal(t, 1, strings.Count(disk, "keychain:gcx:v2:"))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestBoundKeychainWriteUnavailableRotationAbortsWithoutWarning(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	cfg := boundStackTestConfig(server, "")
	cfg.Stacks["default"].Grafana.User = "alice"
	cfg.Stacks["default"].Grafana.Password = "old-password"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	source, err := canonicalConfigSource(path)
	require.NoError(t, err)
	passwordBinding := stackOwner("default", &StackConfig{
		sourceIdentity: source,
		Grafana:        &GrafanaConfig{Server: server, User: "alice"},
	}).binding(credentials.FieldGrafanaPassword)
	oldAccount := storedBoundAccount(t, store, passwordBinding, "old-password")
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	loaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	loaded.Stacks["default"].Grafana.APIToken = "new-token"
	loaded.Stacks["default"].Grafana.Password = "new-password"
	store.setErr = credentials.ErrUnavailable
	store.setErrValue = "new-password"
	logger := &boundTestLogger{}
	ctx := logging.Context(context.Background(), logger)

	err = Write(ctx, ExplicitConfigFile(path), loaded)
	require.ErrorContains(t, err, "cannot replace credential")
	assert.Empty(t, logger.warnings, "an aborted rotation must not claim plaintext was committed")
	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Equal(t, "old-password", store.entries[oldAccount])
	tokenBinding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	assertNoStoredBoundAccount(t, store, tokenBinding)
}

func TestBoundKeychainFallbackWarningRunsOnlyAfterSuccessfulCommit(t *testing.T) {
	logger := &boundTestLogger{}
	var warnings strings.Builder
	txn := newKeychainWriteTransaction(newBoundTestStore(), logger)
	txn.warnUnavailableOnce = func(emit func()) { emit() }
	txn.plaintextFallback = true

	require.NoError(t, txn.commit(&warnings))
	assert.Equal(t, "Warning: credential store unavailable; credentials remain in plaintext on disk; install or unlock your OS credential store (Keychain, Credential Manager, or Secret Service) to enable encrypted credential storage\n", warnings.String())
	assert.Empty(t, logger.warnings, "the request-scoped warning must not be duplicated through structured logging")

	txn = newKeychainWriteTransaction(newBoundTestStore(), logger)
	txn.warnUnavailableOnce = func(emit func()) { emit() }
	txn.plaintextFallback = true

	require.NoError(t, txn.commit(nil))
	require.Equal(t, []string{"credential store unavailable; credentials remain in plaintext on disk"}, logger.warnings)

	logger.warnings = nil
	store := newBoundTestStore()
	store.deleteErr = errors.New("injected cleanup failure")
	txn = newKeychainWriteTransaction(store, logger)
	txn.warnUnavailableOnce = func(emit func()) { emit() }
	txn.plaintextFallback = true
	txn.deferDelete("old-account", "stack:default", credentials.FieldGrafanaToken)

	require.Error(t, txn.commit(nil))
	assert.NotContains(t, logger.warnings, "credential store unavailable; credentials remain in plaintext on disk",
		"a failed commit must not claim plaintext fallback succeeded")
}

func TestBoundKeychainUnavailableReplacementPreservesOldGeneration(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "old-token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	oldAccount := storedBoundAccount(t, store, binding, "old-token")
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.APIToken = "new-token"
	store.getErr = credentials.ErrUnavailable
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "cannot replace credential")
	rawAfter, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, rawBefore, rawAfter)
	assert.Equal(t, "old-token", store.entries[oldAccount])
	assertBoundAccountCount(t, store, binding, 1)
	assert.Equal(t, "new-token", cfg.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainDirectorySyncFailureKeepsBothGenerations(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "old-token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)
	oldAccount := storedBoundAccount(t, store, binding, "old-token")
	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	cfg.Stacks["default"].Grafana.APIToken = "new-token"

	originalSync := syncConfigDirectory
	syncConfigDirectory = func(string) error { return errors.New("injected directory sync failure") }
	t.Cleanup(func() { syncConfigDirectory = originalSync })
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	var durabilityErr *configDurabilityError
	require.ErrorAs(t, err, &durabilityErr)
	assert.Equal(t, "old-token", store.entries[oldAccount])
	assertBoundAccountCount(t, store, binding, 2)
	assert.Equal(t, "new-token", cfg.Stacks["default"].Grafana.APIToken)

	syncConfigDirectory = originalSync
	loaded, loadErr := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, loadErr)
	assert.Equal(t, "new-token", loaded.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainStaleWriterRejectedWithoutSideEffects(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	server := "https://example.invalid"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig(server, "old-token")))
	binding := boundStackTestBinding(t, path, "default", server, credentials.FieldGrafanaToken)

	writerA, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	writerB, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	writerA.Stacks["default"].Grafana.APIToken = "writer-a-token"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), writerA))

	sourceBefore := writerB.Stacks["default"].sourceIdentity
	writerB.CurrentContext = "default"
	err = Write(context.Background(), ExplicitConfigFile(path), writerB)
	require.ErrorContains(t, err, "config changed since it was loaded")
	assert.Equal(t, sourceBefore, writerB.Stacks["default"].sourceIdentity)
	assert.Equal(t, "old-token", writerB.Stacks["default"].Grafana.APIToken)
	assertBoundAccountCount(t, store, binding, 1)

	final, loadErr := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, loadErr)
	assert.Equal(t, "writer-a-token", final.Stacks["default"].Grafana.APIToken)
}

func TestBoundKeychainFutureVersionAppearingAfterLoadIsRejected(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), boundStackTestConfig("https://example.invalid", "old-token")))
	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	future := []byte("version: 999\ncontexts: {}\ncurrent-context: \"\"\n")
	require.NoError(t, os.WriteFile(path, future, 0o600))
	cfg.Stacks["default"].Grafana.APIToken = "new-token"
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "unsupported config version")
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, future, raw)
}

func TestBoundKeychainInventoryDeletesOrphanAndNonCurrentOwners(t *testing.T) {
	for _, tc := range []struct {
		name        string
		withContext bool
	}{
		{name: "orphan"},
		{name: "non-current", withContext: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newBoundTestStore()
			useBoundTestStore(t, store)
			path := filepath.Join(t.TempDir(), "config.yaml")
			server := "https://unused.invalid"
			binding := boundStackTestBinding(t, path, "unused", server, credentials.FieldGrafanaToken)
			account := credentials.BoundAccountKey(binding)
			store.entries[account] = "unused-token"
			contexts := "  current: {}\n"
			if tc.withContext {
				contexts += "  unused:\n    stack: unused\n"
			}
			contents := fmt.Sprintf("version: 1\nstacks:\n  unused:\n    grafana:\n      server: %s\n      token: %s\ncontexts:\n%scurrent-context: current\n", server, credentials.FormatBoundSentinel(binding), contexts)
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

			cfg, err := Load(context.Background(), ExplicitConfigFile(path))
			require.NoError(t, err)
			assert.Empty(t, store.gets, "non-current inventory must not resolve the secret")
			delete(cfg.Stacks, "unused")
			require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
			assert.Contains(t, store.deletes, account)
			assert.NotContains(t, store.entries, account)
		})
	}
}

func TestRawLocalSymlinkLoadIsRejectedBeforeKeychainAccess(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	target := filepath.Join(t.TempDir(), "victim.yaml")
	require.NoError(t, os.WriteFile(target, []byte("version: 1\ncontexts: {}\ncurrent-context: \"\"\n"), 0o600))
	link := filepath.Join(t.TempDir(), LocalConfigFileName)
	require.NoError(t, os.Symlink(target, link))
	ctx := ContextWithConfigSource(context.Background(), ConfigSource{Path: link, Type: "local"})
	_, err := Load(ctx, ExplicitConfigFile(link))
	require.ErrorContains(t, err, "symlinks are not allowed")
	assert.Empty(t, store.gets)
	assert.Empty(t, store.sets)
	assert.Empty(t, store.deletes)
}

func TestCredentialDestinationURLNormalizationPreservesSecurityRelevantBytes(t *testing.T) {
	assert.NotEqual(t, normalizeCredentialURL("https://example.invalid/a%2Fb", ""), normalizeCredentialURL("https://example.invalid/a/b", ""))
	assert.NotEqual(t, normalizeCredentialURL("https://example.invalid/path?tenant=a", ""), normalizeCredentialURL("https://example.invalid/path?tenant=b", ""))
	assert.NotEqual(t, normalizeCredentialURL("https://alice@example.invalid/path", ""), normalizeCredentialURL("https://bob@example.invalid/path", ""))
	assert.NotEqual(t, normalizeCredentialURL("https://example.invalid/path?tenant=a/", ""), normalizeCredentialURL("https://example.invalid/path?tenant=a", ""))
	assert.Equal(t, normalizeCredentialURL("HTTPS://EXAMPLE.INVALID:443/path/", ""), normalizeCredentialURL("https://example.invalid/path", ""))
}

func TestBoundPasswordBindingIncludesExactUsername(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	stack := &StackConfig{Grafana: &GrafanaConfig{Server: "https://example.invalid", User: "alice"}}
	source, err := canonicalConfigSource(path)
	require.NoError(t, err)
	stack.sourceIdentity = source
	alice := stackOwner("default", stack).binding(credentials.FieldGrafanaPassword)
	stack.Grafana.User = " alice "
	spaced := stackOwner("default", stack).binding(credentials.FieldGrafanaPassword)
	stack.Grafana.User = "bob"
	bob := stackOwner("default", stack).binding(credentials.FieldGrafanaPassword)
	assert.NotEqual(t, alice.Destination, spaced.Destination)
	assert.NotEqual(t, alice.Destination, bob.Destination)
}

func TestBoundCredentialTLSAndUsernameOverridesClearStoredSecrets(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := boundStackTestConfig("https://example.invalid", "api-token")
	cfg.Stacks["default"].Grafana.User = "alice"
	cfg.Stacks["default"].Grafana.Password = "password"
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	t.Run("username", func(t *testing.T) {
		t.Setenv("GRAFANA_USER", "bob")
		loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
			return ParseEnvIntoContext(cfg.Contexts["default"])
		})
		require.NoError(t, err)
		assert.Empty(t, loaded.Contexts["default"].Grafana.Password)
		assert.Equal(t, "api-token", loaded.Contexts["default"].Grafana.APIToken)
		assert.Equal(t, "password", loaded.Stacks["default"].Grafana.Password,
			"runtime rejection must not mutate the persisted stack view")
	})

	t.Run("CA file", func(t *testing.T) {
		caPath := filepath.Join(t.TempDir(), "ca.pem")
		require.NoError(t, os.WriteFile(caPath, []byte("test CA"), 0o600))
		t.Setenv("GRAFANA_TLS_CA_FILE", caPath)
		loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(cfg *Config) error {
			return ParseEnvIntoContext(cfg.Contexts["default"])
		})
		require.NoError(t, err)
		assert.Empty(t, loaded.Contexts["default"].Grafana.APIToken)
		assert.Empty(t, loaded.Contexts["default"].Grafana.Password)
		assert.Equal(t, "api-token", loaded.Stacks["default"].Grafana.APIToken,
			"runtime rejection must not mutate the persisted stack view")
		assert.Equal(t, "password", loaded.Stacks["default"].Grafana.Password,
			"runtime rejection must not mutate the persisted stack view")
	})
}

func TestTLSFileContentChangeRejectsCredentialBeforeKeychainGet(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, []byte("CA generation one"), 0o600))
	cfg := boundStackTestConfig("https://example.invalid", "token")
	cfg.Stacks["default"].Grafana.TLS = &TLS{CAFile: caPath}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	store.gets = nil
	require.NoError(t, os.WriteFile(caPath, []byte("CA generation two"), 0o600))

	loaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Empty(t, loaded.Stacks["default"].Grafana.APIToken)
	assert.Empty(t, store.gets, "changed trust material must reject the sentinel before keychain access")
}

func TestTLSFileChangeAfterCredentialResolutionUsesCapturedBytes(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, []byte("CA generation one"), 0o600))
	cfg := boundStackTestConfig("https://example.invalid", "token")
	cfg.Stacks["default"].Grafana.TLS = &TLS{CAFile: caPath}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))
	loaded, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	require.Equal(t, "token", loaded.Stacks["default"].Grafana.APIToken)
	require.NoError(t, os.WriteFile(caPath, []byte("CA generation two"), 0o600))

	restConfig, err := loaded.Contexts["default"].ToRESTConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("CA generation one"), restConfig.CAData)
}

func TestTLSPathOverrideRecapturesEffectiveBytesBeforeTransport(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	originalCAPath := filepath.Join(dir, "original-ca.pem")
	overrideCAPath := filepath.Join(dir, "override-ca.pem")
	require.NoError(t, os.WriteFile(originalCAPath, []byte("original CA"), 0o600))
	require.NoError(t, os.WriteFile(overrideCAPath, []byte("override CA before swap"), 0o600))

	cfg := boundStackTestConfig("https://example.invalid", "token")
	cfg.Stacks["default"].Grafana.TLS = &TLS{CAFile: originalCAPath}
	require.NoError(t, Write(context.Background(), ExplicitConfigFile(path), cfg))

	t.Setenv("GRAFANA_TOKEN", "token")
	t.Setenv("GRAFANA_TLS_CA_FILE", overrideCAPath)
	loaded, err := Load(context.Background(), ExplicitConfigFile(path), func(runtime *Config) error {
		return ParseEnvIntoContext(runtime.Contexts["default"])
	})
	require.NoError(t, err)
	require.Equal(t, "token", loaded.Contexts["default"].Grafana.APIToken)

	// The credential binding evaluated the environment-selected path. A swap
	// after Load must not change the TLS bytes used by the transport.
	require.NoError(t, os.WriteFile(overrideCAPath, []byte("override CA after swap"), 0o600))
	restConfig, err := loaded.Contexts["default"].ToRESTConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("override CA before swap"), restConfig.CAData)
}

func TestWriteDoesNotReplaceConfigCreatedAfterMissingLoad(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.ErrorIs(t, err, os.ErrNotExist)
	external := []byte("version: 1\ncontexts:\n  external: {}\ncurrent-context: external\n")
	require.NoError(t, os.WriteFile(path, external, 0o600))
	cfg.SetStack("default", StackConfig{Grafana: &GrafanaConfig{Server: "https://example.invalid", APIToken: "token"}})
	cfg.SetContext("default", true, Context{Stack: "default"})

	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "created since it was loaded")
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, external, raw)
	assert.Empty(t, store.sets)
}

func TestLocalConfigCannotPairRepositoryDestinationWithEnvironmentCredential(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		secretEnv  string
		secret     string
		credential func(Config) string
		consume    func(Config) error
	}{
		{
			name:      "Grafana token",
			yaml:      "version: 1\nstacks:\n  default:\n    grafana:\n      server: https://attacker.invalid\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n",
			secretEnv: "GRAFANA_TOKEN", secret: "exported-token",
			credential: func(cfg Config) string { return cfg.Contexts["default"].Grafana.APIToken },
			consume: func(cfg Config) error {
				_, err := cfg.Contexts["default"].ToRESTConfig(context.Background())
				return err
			},
		},
		{
			name:      "Grafana password",
			yaml:      "version: 1\nstacks:\n  default:\n    grafana:\n      server: https://attacker.invalid\n      user: attacker\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n",
			secretEnv: "GRAFANA_PASSWORD", secret: "exported-password",
			credential: func(cfg Config) string { return cfg.Contexts["default"].Grafana.Password },
			consume: func(cfg Config) error {
				_, err := cfg.Contexts["default"].ToRESTConfig(context.Background())
				return err
			},
		},
		{
			name:      "Cloud token",
			yaml:      "version: 1\ncloud:\n  attacker:\n    api-url: https://attacker.invalid\ncontexts:\n  default:\n    cloud: attacker\ncurrent-context: default\n",
			secretEnv: "GRAFANA_CLOUD_TOKEN", secret: "exported-cloud-token",
			credential: func(cfg Config) string { return cfg.Contexts["default"].CloudEntry.Token },
			consume: func(cfg Config) error {
				_, err := cfg.Contexts["default"].CloudEntry.ResolveToken()
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), LocalConfigFileName)
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
			t.Setenv(tt.secretEnv, tt.secret)
			ctx := ContextWithConfigSource(context.Background(), ConfigSource{Path: path, Type: "local"})
			cfg, err := Load(ctx, ExplicitConfigFile(path), func(cfg *Config) error {
				return ParseEnvIntoContext(cfg.Contexts["default"])
			})
			require.NoError(t, err)
			assert.Empty(t, tt.credential(cfg))
			consumeErr := tt.consume(cfg)
			require.Error(t, consumeErr)
			var rejected CredentialRejectedError
			require.ErrorAs(t, consumeErr, &rejected)
			assert.Contains(t, consumeErr.Error(), "before network use")
		})
	}
}

func TestExplicitlySelectedLocalConfigMayUseEnvironmentCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), LocalConfigFileName)
	require.NoError(t, os.WriteFile(path, []byte("version: 1\nstacks:\n  default:\n    grafana:\n      server: https://repo.invalid\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n"), 0o600))
	t.Setenv("GRAFANA_SERVER", "https://user-authorized.invalid")
	t.Setenv("GRAFANA_TOKEN", "exported-token")
	ctx := ContextWithConfigSource(context.Background(), ConfigSource{Path: path, Type: "explicit"})
	cfg, err := Load(ctx, ExplicitConfigFile(path), func(cfg *Config) error {
		return ParseEnvIntoContext(cfg.Contexts["default"])
	})
	require.NoError(t, err)
	assert.Equal(t, "https://user-authorized.invalid", cfg.Contexts["default"].Grafana.Server)
	assert.Equal(t, "exported-token", cfg.Contexts["default"].Grafana.APIToken)
}

func TestLocalSourceClassificationSurvivesFileRemovalBeforeEnvironmentOverride(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	home := t.TempDir()
	work := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv(ConfigFileEnvVar, "")
	t.Setenv("GRAFANA_TOKEN", "exported-token")
	t.Chdir(work)
	userPath := filepath.Join(home, ".config", StandardConfigFolder, StandardConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o755))
	require.NoError(t, os.WriteFile(userPath, []byte("version: 1\ncontexts: {}\ncurrent-context: \"\"\n"), 0o600))
	localPath := filepath.Join(work, LocalConfigFileName)
	require.NoError(t, os.WriteFile(localPath, []byte("version: 1\nstacks:\n  default:\n    grafana:\n      server: https://attacker.invalid\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n"), 0o600))

	cfg, err := LoadLayered(context.Background(), "", func(cfg *Config) error {
		require.NoError(t, os.Remove(localPath))
		return ParseEnvIntoContext(cfg.Contexts["default"])
	})
	require.NoError(t, err)
	assert.Empty(t, cfg.Contexts["default"].Grafana.APIToken)
	assert.Empty(t, store.gets)
}

func TestLocalConfigCannotSelectExternalMTLSCredentials(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		override Override
	}{
		{
			name: "file fields",
			yaml: "version: 1\nstacks:\n  default:\n    grafana:\n      server: https://attacker.invalid\n      tls:\n        cert-file: /tmp/user-client.crt\n        key-file: /tmp/user-client.key\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n",
		},
		{
			name: "environment fields",
			yaml: "version: 1\nstacks:\n  default:\n    grafana:\n      server: https://attacker.invalid\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n",
			override: func(cfg *Config) error {
				t.Setenv("GRAFANA_TLS_CERT_FILE", "/tmp/user-client.crt")
				t.Setenv("GRAFANA_TLS_KEY_FILE", "/tmp/user-client.key")
				return ParseEnvIntoContext(cfg.Contexts["default"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newBoundTestStore()
			useBoundTestStore(t, store)
			path := filepath.Join(t.TempDir(), LocalConfigFileName)
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
			ctx := ContextWithConfigSource(context.Background(), ConfigSource{Path: path, Type: "local"})
			var overrides []Override
			if tt.override != nil {
				overrides = append(overrides, tt.override)
			}
			_, err := Load(ctx, ExplicitConfigFile(path), overrides...)
			require.ErrorContains(t, err, "cannot select external TLS client credential files")
			assert.Empty(t, store.gets)
		})
	}
}
