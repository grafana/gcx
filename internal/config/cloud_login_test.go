package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveCloudConfigDoesNotReplaceConcurrentCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	external := []byte("version: 1\ncontexts:\n  external: {}\ncurrent-context: external\n")
	calls := 0
	source := func() (string, error) {
		calls++
		if calls == 2 {
			if err := os.WriteFile(path, external, 0o600); err != nil {
				return "", err
			}
		}
		return path, nil
	}

	_, _, err := config.SaveCloudConfig(context.Background(), source, "default", &config.CloudEntry{Token: "new-token"})
	require.ErrorContains(t, err, "created since it was loaded")
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, external, raw)
}

// TestSaveCloudConfigPreservesStack verifies that re-authenticating (which
// writes fresh cloud auth fields) refreshes the context's existing cloud entry
// in place and does not drop the previously configured stack selection.
func TestSaveCloudConfigPreservesStack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetStack("default", config.StackConfig{Slug: "mystack"})
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		Token:    "old-token",
		OAuthUrl: "https://old.example",
	})
	seed.SetContext(config.DefaultContextName, true, config.Context{
		Stack: "default",
		Cloud: "grafana-com",
	})
	if err := config.Write(ctx, source, seed); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	newCloud := &config.CloudEntry{
		Token:    "new-token",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	}
	contextName, entryName, err := config.SaveCloudConfig(ctx, source, "", newCloud)
	if err != nil {
		t.Fatalf("SaveCloudConfig: %v", err)
	}
	if contextName != config.DefaultContextName {
		t.Errorf("context name: got %q, want %q", contextName, config.DefaultContextName)
	}
	if entryName != "grafana-com" {
		t.Errorf("entry name: got %q, want %q (existing ref must be refreshed in place)", entryName, "grafana-com")
	}

	got, err := config.Load(ctx, source)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cur := got.Contexts[config.DefaultContextName]
	if cur.Cloud != "grafana-com" {
		t.Errorf("cloud ref not preserved: got %q, want %q", cur.Cloud, "grafana-com")
	}
	if cur.CloudEntry == nil || cur.CloudEntry.Token != "new-token" {
		t.Errorf("Token not updated: got %+v, want token %q", cur.CloudEntry, "new-token")
	}
	if got := cur.ResolveStackSlug(); got != "mystack" {
		t.Errorf("stack slug not preserved: got %q, want %q", got, "mystack")
	}
}

func TestSaveCloudConfigCollisionDoesNotReplaceSharedEntry(t *testing.T) {
	// Two different CAPs against the same host: a login from a context with
	// no cloud binding must not quietly replace the host-named entry other
	// contexts share — it gets a context-suffixed entry instead. A login with
	// the SAME credential still dedups onto the shared entry.
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetStack("prod", config.StackConfig{})
	seed.SetCloudEntry("grafana-com", config.CloudEntry{Token: "org-wide-cap"})
	seed.SetContext("prod", true, config.Context{Stack: "prod", Cloud: "grafana-com"})
	seed.SetContext("ci", false, config.Context{})
	require.NoError(t, config.Write(ctx, source, seed))

	// Different credential → suffixed entry, shared entry untouched.
	_, entryName, err := config.SaveCloudConfig(ctx, source, "ci", &config.CloudEntry{Token: "stack-scoped-cap"})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-ci", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	got.ResolveContext("prod")
	assert.Equal(t, "org-wide-cap", got.Contexts["prod"].CloudEntry.Token,
		"shared entry must not be replaced by another context's login")
	assert.Equal(t, "stack-scoped-cap", got.Contexts["ci"].CloudEntry.Token)

	// Same credential from yet another context → dedups onto the shared entry.
	_, entryName, err = config.SaveCloudConfig(ctx, source, "other", &config.CloudEntry{Token: "org-wide-cap"})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com", entryName)
}

func TestSaveCloudConfigSharedEntryUsesCopyOnWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		Token:    "shared-cap",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	seed.SetContext("prod", true, config.Context{Cloud: "grafana-com"})
	seed.SetContext("staging", false, config.Context{Cloud: "grafana-com"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfig(ctx, source, "staging", &config.CloudEntry{
		Token:    "staging-cap",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-staging", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	got.ResolveContext("prod")
	got.ResolveContext("staging")
	assert.Equal(t, "grafana-com", got.Contexts["prod"].Cloud)
	assert.Equal(t, "shared-cap", got.Contexts["prod"].CloudEntry.Token)
	assert.Equal(t, "grafana-com-staging", got.Contexts["staging"].Cloud)
	assert.Equal(t, "staging-cap", got.Contexts["staging"].CloudEntry.Token)
}

func TestSaveCloudConfigSafetyReservesEffectiveLayerNames(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		Token:    "shared-cap",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	seed.SetContext("prod", true, config.Context{Cloud: "grafana-com"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfigWithSafety(
		ctx,
		source,
		"prod",
		&config.CloudEntry{
			Token:    "prod-cap",
			OAuthUrl: "https://grafana.com",
			APIUrl:   "https://grafana.com",
		},
		config.CloudMutationSafety{
			SharedInEffectiveConfig: true,
			ReservedEntryNames:      []string{"grafana-com", "grafana-com-prod"},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-prod-2", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Equal(t, "shared-cap", got.Cloud["grafana-com"].Token)
	assert.Equal(t, "prod-cap", got.Cloud[entryName].Token)
	assert.Equal(t, entryName, got.Contexts["prod"].Cloud)
}

func TestSaveCloudConfigSafetyReservesUnboundEffectiveLayerName(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetContext("prod", true, config.Context{})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfigWithSafety(
		ctx,
		source,
		"prod",
		&config.CloudEntry{
			Token:    "prod-cap",
			OAuthUrl: "https://grafana.com",
			APIUrl:   "https://grafana.com",
		},
		config.CloudMutationSafety{ReservedEntryNames: []string{"grafana-com"}},
	)
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-prod", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Nil(t, got.Cloud["grafana-com"], "a name owned by another effective layer must not be shadowed")
	assert.Equal(t, "prod-cap", got.Cloud[entryName].Token)
	assert.Equal(t, entryName, got.Contexts["prod"].Cloud)
}

func TestSaveCloudConfigSafetyDoesNotReuseShadowedRawEntry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		Token:    "same-cap",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	seed.SetContext("prod", true, config.Context{})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfigWithSafety(
		ctx,
		source,
		"prod",
		&config.CloudEntry{Token: "same-cap", OAuthUrl: "https://grafana.com", APIUrl: "https://grafana.com"},
		config.CloudMutationSafety{
			ReservedEntryNames: []string{"grafana-com"},
			ForeignEntryNames:  []string{"grafana-com"},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-prod", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Equal(t, "same-cap", got.Cloud["grafana-com"].Token)
	assert.Equal(t, entryName, got.Contexts["prod"].Cloud)
}

func TestSaveCloudConfigEndpointChangeUsesCopyOnWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		Token:    "same-cap",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	seed.SetContext("prod", true, config.Context{Cloud: "grafana-com"})
	seed.SetContext("ops", false, config.Context{Cloud: "grafana-com"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfig(ctx, source, "ops", &config.CloudEntry{
		Token:    "same-cap",
		OAuthUrl: "https://grafana-ops.com",
		APIUrl:   "https://grafana-ops.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-ops", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Equal(t, "https://grafana.com", got.Cloud["grafana-com"].APIUrl)
	assert.Equal(t, "https://grafana-ops.com", got.Cloud[entryName].APIUrl)
}

func TestSaveCloudConfigOAuthMetadataChangeUsesCopyOnWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		OAuthToken:          "shared-oauth",
		OAuthTokenExpiresAt: "2099-01-01T00:00:00Z",
		OAuthScopes:         []string{"stacks:read"},
		OAuthUrl:            "https://grafana.com",
		APIUrl:              "https://grafana.com",
	})
	seed.SetContext("prod", true, config.Context{Cloud: "grafana-com"})
	seed.SetContext("staging", false, config.Context{Cloud: "grafana-com"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfig(ctx, source, "staging", &config.CloudEntry{
		OAuthToken:          "shared-oauth",
		OAuthTokenExpiresAt: "2099-02-01T00:00:00Z",
		OAuthScopes:         []string{"stacks:read", "fleet-management:read"},
		OAuthUrl:            "https://grafana.com",
		APIUrl:              "https://grafana.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-staging", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Equal(t, "2099-01-01T00:00:00Z", got.Cloud["grafana-com"].OAuthTokenExpiresAt)
	assert.Equal(t, []string{"stacks:read"}, got.Cloud["grafana-com"].OAuthScopes)
	assert.Equal(t, "2099-02-01T00:00:00Z", got.Cloud[entryName].OAuthTokenExpiresAt)
	assert.ElementsMatch(t, []string{"stacks:read", "fleet-management:read"}, got.Cloud[entryName].OAuthScopes)
}

func TestSaveCloudConfigOAuthScopeOrderDoesNotTriggerCopyOnWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		OAuthToken:          "shared-oauth",
		OAuthTokenExpiresAt: "2099-01-01T00:00:00Z",
		OAuthScopes:         []string{"stacks:read", "fleet-management:read"},
		OAuthUrl:            "https://grafana.com",
		APIUrl:              "https://grafana.com",
	})
	seed.SetContext("prod", true, config.Context{Cloud: "grafana-com"})
	seed.SetContext("staging", false, config.Context{Cloud: "grafana-com"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfig(ctx, source, "staging", &config.CloudEntry{
		OAuthToken:          "shared-oauth",
		OAuthTokenExpiresAt: "2099-01-01T00:00:00Z",
		OAuthScopes:         []string{"fleet-management:read", "stacks:read", "stacks:read"},
		OAuthUrl:            "https://grafana.com",
		APIUrl:              "https://grafana.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Len(t, got.Cloud, 1)
}

func TestSaveCloudConfigUniqueEntryUpdatesInPlace(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{Token: "old-cap"})
	seed.SetContext("only", true, config.Context{Cloud: "grafana-com"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfig(ctx, source, "only", &config.CloudEntry{Token: "new-cap"})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Len(t, got.Cloud, 1)
	assert.Equal(t, "new-cap", got.Cloud["grafana-com"].Token)
}

func TestSaveCloudConfigCopyOnWriteNameCollisionIsSafe(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetCloudEntry("grafana-com", config.CloudEntry{Token: "shared-cap"})
	seed.SetCloudEntry("grafana-com-staging", config.CloudEntry{Token: "occupied-cap"})
	seed.SetContext("prod", true, config.Context{Cloud: "grafana-com"})
	seed.SetContext("staging", false, config.Context{Cloud: "grafana-com"})
	seed.SetContext("other", false, config.Context{Cloud: "grafana-com-staging"})
	require.NoError(t, config.Write(ctx, source, seed))

	_, entryName, err := config.SaveCloudConfig(ctx, source, "staging", &config.CloudEntry{Token: "new-cap"})
	require.NoError(t, err)
	assert.Equal(t, "grafana-com-staging-2", entryName)

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	assert.Equal(t, "occupied-cap", got.Cloud["grafana-com-staging"].Token)
	assert.Equal(t, "new-cap", got.Cloud[entryName].Token)
}

func TestMergeCloudIntoSwitchingAuthMethodClearsTheOther(t *testing.T) {
	// An entry holds one credential: an OAuth login over a CAP-token entry
	// clears the CAP token (and vice versa), so a stale credential never
	// shadows the fresh one.
	fromOAuth := config.MergeCloudInto(
		&config.CloudEntry{Token: "cap-token"},
		&config.CloudEntry{OAuthToken: "oauth-token", OAuthTokenExpiresAt: "2099-01-01T00:00:00Z", OAuthScopes: []string{"stacks:read"}},
	)
	assert.Empty(t, fromOAuth.Token)
	assert.Equal(t, "oauth-token", fromOAuth.OAuthToken)
	assert.Equal(t, "2099-01-01T00:00:00Z", fromOAuth.OAuthTokenExpiresAt)
	assert.Equal(t, []string{"stacks:read"}, fromOAuth.OAuthScopes)

	fromCAP := config.MergeCloudInto(
		&config.CloudEntry{OAuthToken: "oauth-token", OAuthTokenExpiresAt: "2099-01-01T00:00:00Z", OAuthScopes: []string{"stacks:read"}},
		&config.CloudEntry{Token: "cap-token"},
	)
	assert.Equal(t, "cap-token", fromCAP.Token)
	assert.Empty(t, fromCAP.OAuthToken)
	assert.Empty(t, fromCAP.OAuthTokenExpiresAt)
	assert.Empty(t, fromCAP.OAuthScopes)
}

func TestLoginServerChangeInvalidatesStoredSMToken(t *testing.T) {
	store := withFakeStore(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	seed := config.Config{
		Version:        config.ConfigVersion,
		CurrentContext: "default",
		Stacks: map[string]*config.StackConfig{
			"default": {
				Grafana: &config.GrafanaConfig{Server: "https://old.example.invalid", APIToken: "old-api-token", OrgID: 1},
				Providers: map[string]map[string]string{
					"synth": {"sm-url": "https://sm.example.invalid", "sm-token": "old-sm-token"},
				},
			},
		},
		Contexts: map[string]*config.Context{"default": {Stack: "default"}},
	}
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), seed))
	oldSMBinding, err := config.StackBindingForTest(path, "default", "https://old.example.invalid", credentials.FieldSMToken)
	require.NoError(t, err)
	oldSMAccount := storedBoundValue(t, store, oldSMBinding, "old-sm-token")

	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://new.example.invalid",
			ContextName:  "default",
			Target:       login.TargetOnPrem,
			GrafanaToken: "new-api-token",
		},
		Hooks: login.Hooks{
			ConfigSource: config.ExplicitConfigFile(path),
			ValidateFn: func(context.Context, login.Options, config.NamespacedRESTConfig) (string, error) {
				return "12.0.0", nil
			},
		},
		RetryState: login.RetryState{AllowOverride: true},
	}
	_, err = login.Run(t.Context(), &opts)
	require.NoError(t, err)
	assert.True(t, store.deleted(oldSMAccount))

	loaded, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.invalid", loaded.Stacks["default"].Grafana.Server)
	assert.Equal(t, "new-api-token", loaded.Stacks["default"].Grafana.APIToken)
	assert.Empty(t, loaded.Stacks["default"].Providers["synth"]["sm-token"])
}

func TestSaveCloudConfigAuthSwitchFailsClosedWhenKeychainUnavailable(t *testing.T) {
	store := withFakeStore(t)
	store.setGetErr(credentials.ErrUnavailable)
	path := filepath.Join(t.TempDir(), "config.yaml")
	oldBinding, err := config.CloudBindingForTest(path, "grafana-com", credentials.FieldOAuthToken)
	require.NoError(t, err)
	oldAccount := credentials.BoundAccountKey(oldBinding)
	store.entries[oldAccount] = "old-oauth-token"
	oldSentinel := credentials.FormatBoundSentinel(oldBinding)

	contents := fmt.Sprintf(`version: 1
cloud:
  grafana-com:
    oauth-token: %s
    oauth-token-expires-at: "2099-01-01T00:00:00Z"
    oauth-url: https://grafana.com
    api-url: https://grafana.com
contexts:
  default:
    cloud: grafana-com
current-context: default
`, oldSentinel)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	_, _, err = config.SaveCloudConfig(t.Context(), config.ExplicitConfigFile(path), "default", &config.CloudEntry{
		Token:    "new-cap",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	require.ErrorIs(t, err, credentials.ErrUnavailable)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, rawBefore, raw)
	assert.False(t, store.deleted(oldAccount))
	assert.Equal(t, "old-oauth-token", store.entries[oldAccount])

	newBinding := oldBinding
	newBinding.Field = credentials.FieldCloudToken
	_, created := store.entries[credentials.BoundAccountKey(newBinding)]
	assert.False(t, created)
}

func TestLoginAuthSwitchFailsClosedWhenKeychainUnavailable(t *testing.T) {
	store := withFakeStore(t)
	store.setGetErr(credentials.ErrUnavailable)
	path := filepath.Join(t.TempDir(), "config.yaml")
	const server = "https://grafana.example.com"
	bindings := map[string]credentials.Binding{}
	for name, field := range map[string]credentials.Field{
		"password":            credentials.FieldGrafanaPassword,
		"oauth-token":         credentials.FieldOAuthToken,
		"oauth-refresh-token": credentials.FieldOAuthRefreshToken,
	} {
		binding, err := config.StackBindingWithUserForTest(path, "default", server, "old-user", field)
		require.NoError(t, err)
		bindings[name] = binding
	}
	for name, binding := range bindings {
		store.entries[credentials.BoundAccountKey(binding)] = "old-" + name
	}

	contents := fmt.Sprintf(`version: 1
stacks:
  default:
    grafana:
      server: %s
      user: old-user
      password: %s
      oauth-token: %s
      oauth-refresh-token: %s
      auth-method: oauth
      org-id: 1
contexts:
  default:
    stack: default
current-context: default
`, server,
		credentials.FormatBoundSentinel(bindings["password"]),
		credentials.FormatBoundSentinel(bindings["oauth-token"]),
		credentials.FormatBoundSentinel(bindings["oauth-refresh-token"]))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	rawBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	opts := login.Options{
		Inputs: login.Inputs{
			Server:       server,
			ContextName:  "default",
			Target:       login.TargetOnPrem,
			GrafanaToken: "new-service-token",
		},
		Hooks: login.Hooks{
			ConfigSource: config.ExplicitConfigFile(path),
			ValidateFn: func(context.Context, login.Options, config.NamespacedRESTConfig) (string, error) {
				return "12.0.0", nil
			},
		},
	}
	_, err = login.Run(t.Context(), &opts)
	require.ErrorIs(t, err, credentials.ErrUnavailable)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, rawBefore, raw)
	for name, binding := range bindings {
		account := credentials.BoundAccountKey(binding)
		assert.False(t, store.deleted(account))
		assert.Equal(t, "old-"+name, store.entries[account])
	}
}

func TestCloudEntryResolveToken(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	tests := []struct {
		name    string
		entry   config.CloudEntry
		want    string
		wantErr string
	}{
		{
			name:  "access policy token wins",
			entry: config.CloudEntry{Token: "cap", OAuthToken: "oauth"},
			want:  "cap",
		},
		{
			name:  "oauth token used when no CAP token",
			entry: config.CloudEntry{OAuthToken: "oauth", OAuthTokenExpiresAt: future},
			want:  "oauth",
		},
		{
			name:  "oauth token without expiry is used",
			entry: config.CloudEntry{OAuthToken: "oauth"},
			want:  "oauth",
		},
		{
			name:    "expired oauth token names the fix",
			entry:   config.CloudEntry{Name: "grafana-com", OAuthToken: "oauth", OAuthTokenExpiresAt: past},
			wantErr: "gcx cloud login",
		},
		{
			name:    "malformed oauth expiry names the fix",
			entry:   config.CloudEntry{Name: "grafana-com", OAuthToken: "oauth", OAuthTokenExpiresAt: "not-a-timestamp"},
			wantErr: "gcx cloud login",
		},
		{
			name:  "no credential",
			entry: config.CloudEntry{APIUrl: "https://grafana.com"},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.entry.ResolveToken()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
