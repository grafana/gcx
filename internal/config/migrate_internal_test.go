package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKeychain is an in-memory credentials.Store for migration tests.
type fakeKeychain struct {
	entries map[string]string
	gets    []string
	getErr  error
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{entries: map[string]string{}}
}

func (f *fakeKeychain) Get(key string) (string, error) {
	f.gets = append(f.gets, key)
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.entries[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeychain) Set(key, value string) error {
	f.entries[key] = value
	return nil
}

func (f *fakeKeychain) Delete(key string) error {
	delete(f.entries, key)
	return nil
}

// withFakeKeychain swaps the keychain backend for the test's lifetime.
func withFakeKeychain(t *testing.T) *fakeKeychain {
	t.Helper()
	store := newFakeKeychain()
	orig := keychainStoreFn
	keychainStoreFn = func() credentials.Store { return store }
	t.Cleanup(func() { keychainStoreFn = orig })
	return store
}

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func writeTrustedUserTestConfig(t *testing.T, contents string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	path := filepath.Join(root, ".config", StandardConfigFolder, StandardConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func storedFakeBoundAccount(t *testing.T, store *fakeKeychain, binding credentials.Binding, value string) {
	t.Helper()
	var matches []string
	for account, stored := range store.entries {
		if credentials.MatchesBoundAccount(account, binding) && stored == value {
			matches = append(matches, account)
		}
	}
	require.Len(t, matches, 1)
}

func assertOnlyAuthorizedLegacyGet(t *testing.T, store *fakeKeychain, legacyAccount string, binding credentials.Binding) {
	t.Helper()
	legacyGets := 0
	for _, account := range store.gets {
		if account == legacyAccount {
			legacyGets++
			continue
		}
		require.True(t, credentials.MatchesBoundAccount(account, binding),
			"unexpected keychain account read: %s", account)
	}
	assert.Equal(t, 1, legacyGets, "the one-time migration must read the authorized predictable account exactly once")
}

func TestIsLegacyConfig(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     bool
	}{
		{
			name: "context with grafana block",
			contents: `
contexts:
  dev:
    grafana:
      server: https://dev.grafana.net
current-context: dev
`,
			want: true,
		},
		{
			name: "context with cloud mapping",
			contents: `
contexts:
  dev:
    cloud:
      token: abc
current-context: dev
`,
			want: true,
		},
		{
			name: "context with legacy datasource field",
			contents: `
contexts:
  dev:
    default-prometheus-datasource: prom
current-context: dev
`,
			want: true,
		},
		{
			name: "version field marks current format",
			contents: `
version: 1
contexts:
  dev: {}
current-context: dev
`,
			want: false,
		},
		{
			name: "top-level stacks marks current format",
			contents: `
stacks:
  dev:
    grafana:
      server: https://dev.grafana.net
contexts:
  dev:
    stack: dev
current-context: dev
`,
			want: false,
		},
		{
			name: "context with stack ref",
			contents: `
contexts:
  dev:
    stack: dev
current-context: dev
`,
			want: false,
		},
		{
			name:     "default empty config",
			contents: defaultEmptyConfigFile,
			want:     false,
		},
		{
			name: "empty context is ambiguous, treated as current",
			contents: `
contexts:
  default: {}
current-context: default
`,
			want: false,
		},
		{
			name: "datasources-only context decodes identically in both formats",
			contents: `
contexts:
  dev:
    datasources:
      prometheus: my-prom
current-context: dev
`,
			want: false,
		},
		{
			name:     "unparseable input defers to the strict decoder",
			contents: "contexts: [not-a-map",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isLegacyConfig([]byte(tc.contents)))
		})
	}
}

const legacyFullConfig = `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
      token: dev-token
    cloud:
      token: shared-cloud-token
    datasources:
      loki: my-loki
    default-prometheus-datasource: legacy-prom
    default-loki-datasource: legacy-loki
    providers:
      slo:
        org-id: "42"
    resources:
      assume-server-dry-run:
        - receivers.notifications.alerting.grafana.app
  prod:
    grafana:
      server: https://prodstack.grafana.net
      token: prod-token
    cloud:
      token: shared-cloud-token
      stack: prodstack
  ops:
    cloud:
      token: ops-cloud-token
      api-url: https://grafana-ops.com
      oauth-url: https://grafana-ops.com
current-context: dev
`

func TestMigrateLegacyConfig(t *testing.T) {
	withFakeKeychain(t)
	path := writeTestConfig(t, legacyFullConfig)

	cfg, err := Load(withConfigLayer(context.Background(), "user"), ExplicitConfigFile(path))
	require.NoError(t, err)

	// Contexts became thin references.
	require.Len(t, cfg.Contexts, 3)
	dev := cfg.Contexts["dev"]
	require.NotNil(t, dev)
	assert.Equal(t, "dev", dev.Stack)
	assert.Equal(t, "grafana-com", dev.Cloud)

	// Stack carries the grafana connection, providers, and resources.
	devStack := cfg.Stacks["dev"]
	require.NotNil(t, devStack)
	assert.Equal(t, "https://devstack.grafana.net", devStack.Grafana.Server)
	assert.Equal(t, "dev-token", devStack.Grafana.APIToken)
	assert.Equal(t, "42", devStack.Providers["slo"]["org-id"])
	assert.Equal(t, []string{"receivers.notifications.alerting.grafana.app"}, dev.AssumeServerDryRun())

	// Legacy Cloud.Stack became the stack entry's slug.
	require.NotNil(t, cfg.Stacks["prod"])
	assert.Equal(t, "prodstack", cfg.Stacks["prod"].Slug)
	assert.Equal(t, "prodstack", cfg.Contexts["prod"].ResolveStackSlug())

	// Identical cloud configs dedup into one host-named entry; the distinct
	// ops environment gets its own entry named after its API URL host.
	assert.Equal(t, "grafana-com", cfg.Contexts["prod"].Cloud)
	require.NotNil(t, cfg.Cloud["grafana-com"])
	assert.Equal(t, "shared-cloud-token", cfg.Cloud["grafana-com"].Token)
	require.NotNil(t, cfg.Cloud["grafana-ops-com"])
	assert.Equal(t, "ops-cloud-token", cfg.Cloud["grafana-ops-com"].Token)
	assert.Equal(t, "https://grafana-ops.com", cfg.Cloud["grafana-ops-com"].APIUrl)
	assert.Equal(t, "grafana-ops-com", cfg.Contexts["ops"].Cloud)

	// The ops context has cloud auth but no grafana: no stack entry.
	assert.Empty(t, cfg.Contexts["ops"].Stack)

	// Legacy datasource fields folded into the map; the map wins on conflict.
	assert.Equal(t, "my-loki", dev.Datasources["loki"])
	assert.Equal(t, "legacy-prom", dev.Datasources["prometheus"])

	assert.Equal(t, "dev", cfg.CurrentContext)

	// The file was rewritten in the new format.
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.False(t, isLegacyConfig(rewritten))
	assert.Contains(t, string(rewritten), "version: 1")
	assert.Contains(t, string(rewritten), "stacks:")

	// A backup of the legacy file exists.
	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.True(t, isLegacyConfig(backup))

	// Reloading the migrated file does not migrate again and yields the same view.
	again, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, int64(ConfigVersion), again.Version)
	require.NotNil(t, again.Contexts["dev"])
	assert.Equal(t, "dev", again.Contexts["dev"].Stack)
	assert.Equal(t, "https://devstack.grafana.net", again.Contexts["dev"].Grafana.Server)
}

func TestMigrateLegacyConfigEntryNameCollision(t *testing.T) {
	withFakeKeychain(t)
	path := writeTrustedUserTestConfig(t, `
contexts:
  alpha:
    cloud:
      token: token-a
  beta:
    cloud:
      token: token-b
current-context: alpha
`)

	cfg, err := Load(withConfigLayer(context.Background(), "user"), ExplicitConfigFile(path))
	require.NoError(t, err)

	// Two distinct tokens against the same host: the first (sorted) context
	// takes the plain host name, the second gets a context-name suffix.
	require.Len(t, cfg.Cloud, 2)
	assert.Equal(t, "grafana-com", cfg.Contexts["alpha"].Cloud)
	assert.Equal(t, "grafana-com-beta", cfg.Contexts["beta"].Cloud)
	assert.Equal(t, "token-a", cfg.Cloud["grafana-com"].Token)
	assert.Equal(t, "token-b", cfg.Cloud["grafana-com-beta"].Token)
}

func TestMigrateLegacyConfigKeychainCopyOnly(t *testing.T) {
	store := withFakeKeychain(t)
	// Simulate a config that already went through the plaintext→keychain
	// migration: sentinels on disk, values under legacy per-context keys.
	store.entries["dev:cloud-token"] = "secret-cloud"
	store.entries["dev:grafana-token"] = "secret-grafana"
	path := writeTrustedUserTestConfig(t, `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
      token: keychain:gcx:dev:grafana-token
    cloud:
      token: keychain:gcx:dev:cloud-token
current-context: dev
`)

	cfg, err := Load(withConfigLayer(context.Background(), "user"), ExplicitConfigFile(path))
	require.NoError(t, err)

	// In-memory view holds resolved plaintext.
	assert.Equal(t, "secret-cloud", cfg.Contexts["dev"].CloudEntry.Token)
	assert.Equal(t, "secret-grafana", cfg.Contexts["dev"].Grafana.APIToken)

	// Values were copied to source-bound owner keys; the legacy keys survive
	// so the .legacy.bak sentinels stay resolvable.
	stackBinding := stackOwner("dev", cfg.Stacks["dev"]).binding(credentials.FieldGrafanaToken)
	cloudBinding := cloudOwner("grafana-com", cfg.Cloud["grafana-com"]).binding(credentials.FieldCloudToken)
	storedFakeBoundAccount(t, store, cloudBinding, "secret-cloud")
	storedFakeBoundAccount(t, store, stackBinding, "secret-grafana")
	assert.Equal(t, "secret-cloud", store.entries["dev:cloud-token"])
	assert.Equal(t, "secret-grafana", store.entries["dev:grafana-token"])

	// The rewritten file contains only opaque source-bound references.
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(rewritten), "keychain:gcx:v2:")
	assert.NotContains(t, string(rewritten), "keychain:gcx:cloud:grafana-com:cloud-token")
	assert.NotContains(t, string(rewritten), "keychain:gcx:stack:dev:grafana-token")
}

func TestMigrateLegacyMissingKeychainCredentialPreservesRejectionEvidence(t *testing.T) {
	tests := map[string]struct {
		legacyYAML    string
		sentinel      string
		rejectedField credentials.Field
		assertContext func(*testing.T, *Context)
	}{
		"missing password cannot become empty Basic authentication": {
			legacyYAML: `
contexts:
  prod:
    grafana:
      server: https://prod.example
      user: admin
      password: keychain:gcx:prod:grafana-password
      org-id: 1
current-context: prod
`,
			sentinel:      "keychain:gcx:prod:grafana-password",
			rejectedField: credentials.FieldGrafanaPassword,
			assertContext: func(t *testing.T, ctx *Context) {
				t.Helper()
				assert.Equal(t, "admin", ctx.Grafana.User)
				assert.Empty(t, ctx.Grafana.Password)
			},
		},
		"missing token cannot downgrade to valid Basic authentication": {
			legacyYAML: `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
      user: admin
      password: valid-basic-password
      org-id: 1
current-context: prod
`,
			sentinel:      "keychain:gcx:prod:grafana-token",
			rejectedField: credentials.FieldGrafanaToken,
			assertContext: func(t *testing.T, ctx *Context) {
				t.Helper()
				assert.Equal(t, "admin", ctx.Grafana.User)
				assert.Equal(t, "valid-basic-password", ctx.Grafana.Password)
				assert.Empty(t, ctx.Grafana.APIToken)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			withFakeKeychain(t)
			path := writeTrustedUserTestConfig(t, test.legacyYAML)

			cfg, err := Load(withConfigLayer(t.Context(), "user"), ExplicitConfigFile(path))
			require.NoError(t, err)
			ctx := cfg.Contexts["prod"]
			require.NotNil(t, ctx)
			require.NotNil(t, ctx.Grafana)
			test.assertContext(t, ctx)

			err = ctx.GrafanaCredentialRejection()
			require.Error(t, err)
			var rejected CredentialRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Equal(t, test.rejectedField, rejected.Field)

			_, err = ctx.ToRESTConfig(t.Context())
			require.ErrorAs(t, err, &rejected)

			onDisk, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(onDisk), "version: 1")
			assert.Contains(t, string(onDisk), test.sentinel)
		})
	}
}

func TestMigrateLegacyConfigRejectsCrossOwnerAndCrossFieldSentinels(t *testing.T) {
	store := withFakeKeychain(t)
	store.entries["victim:grafana-token"] = "victim-token"
	store.entries["attacker:grafana-token"] = "attacker-token"
	path := writeTrustedUserTestConfig(t, `
contexts:
  attacker:
    grafana:
      server: https://attacker.invalid
      token: keychain:gcx:victim:grafana-token
      password: keychain:gcx:attacker:grafana-token
current-context: attacker
`)

	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = Load(withConfigLayer(context.Background(), "user"), ExplicitConfigFile(path))
	require.ErrorContains(t, err, "invalid legacy keychain reference")
	assert.Empty(t, store.gets, "legacy YAML must not select another owner or field's keychain account")

	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, rewritten)
	assert.NotContains(t, string(rewritten), "victim-token")
	assert.NotContains(t, string(rewritten), "attacker-token")
}

func TestMigrateExplicitLegacySentinelConsentIsPathBound(t *testing.T) {
	tests := []struct {
		name string
		load func(*testing.T, context.Context, string) (Config, error)
	}{
		{
			name: "config flag",
			load: func(_ *testing.T, ctx context.Context, path string) (Config, error) {
				return LoadLayered(ctx, path)
			},
		},
		{
			name: "GCX_CONFIG",
			load: func(t *testing.T, ctx context.Context, path string) (Config, error) {
				t.Helper()
				t.Setenv(ConfigFileEnvVar, path)
				return LoadLayered(ctx, "")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := withFakeKeychain(t)
			store.entries["prod:grafana-token"] = "prod-secret"
			store.entries["other:grafana-token"] = "other-secret"
			path := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)

			cfg, err := tc.load(t, t.Context(), path)
			require.NoError(t, err)
			assert.Equal(t, "prod-secret", cfg.Contexts["prod"].Grafana.APIToken)
			binding := stackOwner("prod", cfg.Stacks["prod"]).binding(credentials.FieldGrafanaToken)
			assertOnlyAuthorizedLegacyGet(t, store, "prod:grafana-token", binding)
		})
	}
}

func TestMigrateExplicitLegacySentinelRejectsMismatchedOrInsecureSourceWithoutGet(t *testing.T) {
	t.Run("mismatched owner", func(t *testing.T) {
		store := withFakeKeychain(t)
		store.entries["victim:grafana-token"] = "victim-secret"
		path := writeTestConfig(t, `
contexts:
  attacker:
    grafana:
      server: https://attacker.example
      token: keychain:gcx:victim:grafana-token
current-context: attacker
`)

		_, err := LoadLayered(t.Context(), path)
		require.ErrorContains(t, err, "invalid legacy keychain reference")
		assert.Empty(t, store.gets)
	})

	t.Run("group writable", func(t *testing.T) {
		store := withFakeKeychain(t)
		store.entries["prod:grafana-token"] = "prod-secret"
		path := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)
		require.NoError(t, os.Chmod(path, 0o660))

		_, err := LoadLayered(t.Context(), path)
		require.ErrorContains(t, err, "untrusted config source")
		assert.Empty(t, store.gets)
	})

	t.Run("generic direct load has no consent", func(t *testing.T) {
		store := withFakeKeychain(t)
		store.entries["prod:grafana-token"] = "prod-secret"
		path := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)

		_, err := Load(t.Context(), ExplicitConfigFile(path))
		require.ErrorContains(t, err, "untrusted config source")
		assert.Empty(t, store.gets)
	})
}

func TestMigrateDiscoveredUserLegacySentinelThroughSymlinkedHome(t *testing.T) {
	store := withFakeKeychain(t)
	store.entries["prod:grafana-token"] = "prod-secret"
	realHome := t.TempDir()
	linkRoot := t.TempDir()
	linkedHome := filepath.Join(linkRoot, "home")
	require.NoError(t, os.Symlink(realHome, linkedHome))
	t.Setenv("HOME", linkedHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(linkedHome, ".config"))
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(linkRoot, "no-system"))
	t.Setenv(ConfigFileEnvVar, "")
	t.Chdir(t.TempDir())

	path := filepath.Join(realHome, ".config", StandardConfigFolder, StandardConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
current-context: prod
`), 0o600))

	cfg, err := LoadLayered(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, "prod-secret", cfg.Contexts["prod"].Grafana.APIToken)
	binding := stackOwner("prod", cfg.Stacks["prod"]).binding(credentials.FieldGrafanaToken)
	assertOnlyAuthorizedLegacyGet(t, store, "prod:grafana-token", binding)
}

func TestMigrateAutoDiscoveredSystemLegacySentinelMakesNoGet(t *testing.T) {
	store := withFakeKeychain(t)
	store.entries["prod:grafana-token"] = "prod-secret"
	path := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)

	_, err := Load(withConfigLayer(t.Context(), "system"), ExplicitConfigFile(path))
	require.ErrorContains(t, err, "untrusted config source")
	assert.Empty(t, store.gets)
}

func TestMigrateLocalLegacySentinelCannotReadSameNamedAccount(t *testing.T) {
	store := withFakeKeychain(t)
	store.entries["prod:grafana-token"] = "user-secret"
	path := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://attacker.invalid
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = Load(withConfigLayer(context.Background(), "local"), ExplicitConfigFile(path))
	require.ErrorContains(t, err, "untrusted config source")
	assert.Empty(t, store.gets)
	assert.Equal(t, "user-secret", store.entries["prod:grafana-token"])
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
	_, statErr := os.Stat(path + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestMigrateTrustedLexicalSymlinkCannotReadLegacyAccount(t *testing.T) {
	store := withFakeKeychain(t)
	store.entries["prod:grafana-token"] = "user-secret"
	target := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://repo.invalid
      token: keychain:gcx:prod:grafana-token
current-context: prod
`)
	link := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.Symlink(target, link))

	_, err := Load(withConfigLayer(context.Background(), "user"), ExplicitConfigFile(link))
	require.ErrorContains(t, err, "untrusted config source")
	assert.Empty(t, store.gets)
	assert.Equal(t, "user-secret", store.entries["prod:grafana-token"])
}

func TestMigrateLocalPlaintextDoesNotOverwriteSameNamedLegacyAccount(t *testing.T) {
	store := withFakeKeychain(t)
	store.entries["prod:grafana-token"] = "user-secret"
	path := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://repo.invalid
      token: repo-token
current-context: prod
`)

	cfg, err := Load(withConfigLayer(context.Background(), "local"), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, "repo-token", cfg.Contexts["prod"].Grafana.APIToken)
	assert.Equal(t, "user-secret", store.entries["prod:grafana-token"])
	assert.NotContains(t, store.gets, "prod:grafana-token")

	binding := boundStackTestBinding(t, path, "prod", "https://repo.invalid", credentials.FieldGrafanaToken)
	storedFakeBoundAccount(t, store, binding, "repo-token")
	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.Contains(t, string(backup), "repo-token")
}

func TestMigrateLegacyConfigPlaintextBackupDoesNotWriteLegacyAccount(t *testing.T) {
	store := withFakeKeychain(t)
	path := writeTestConfig(t, `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
      token: plaintext-secret
current-context: dev
`)

	_, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	// The exact 0600 rollback backup retains the original plaintext. Migration
	// never writes predictable legacy account names, which could overwrite a
	// credential owned by another config source.
	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.Contains(t, string(backup), "plaintext-secret")
	_, hasLegacyAccount := store.entries["dev:grafana-token"]
	assert.False(t, hasLegacyAccount)

	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(rewritten), "plaintext-secret")
}

func TestMigrateLegacyConfigKeychainUnavailable(t *testing.T) {
	store := withFakeKeychain(t)
	store.getErr = credentials.ErrUnavailable
	legacy := `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
      token: plaintext-secret
    cloud:
      token: keychain:gcx:dev:cloud-token
current-context: dev
`
	path := writeTrustedUserTestConfig(t, legacy)

	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(withConfigLayer(context.Background(), "user"), &warnings)
	cfg, err := Load(ctx, ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Equal(t, "plaintext-secret", cfg.Contexts["dev"].Grafana.APIToken)
	assert.Empty(t, cfg.Contexts["dev"].CloudEntry.Token)

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(onDisk), "transient failure must leave the retryable legacy source untouched")
	_, statErr := os.Stat(path + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	assert.Contains(t, warnings.String(), "a legacy credential could not be read from the credential store")
	err = Write(context.Background(), ExplicitConfigFile(path), cfg)
	require.ErrorContains(t, err, "legacy config migration is deferred")
	require.ErrorContains(t, err, "resolve the reported migration blocker")

	// The unchanged legacy source retries and rekeys successfully once the
	// keychain is available again.
	store.getErr = nil
	store.entries["dev:cloud-token"] = "recovered"
	recovered, err := Load(withConfigLayer(context.Background(), "user"), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, "recovered", recovered.Contexts["dev"].CloudEntry.Token)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(rewritten), "keychain:gcx:v2:")
	assert.NotContains(t, string(rewritten), "keychain:gcx:dev:cloud-token")
}

func TestMigrateLegacyConfigReadOnlyFile(t *testing.T) {
	withFakeKeychain(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	legacy := `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
current-context: dev
`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(context.Background(), &warnings)
	cfg, err := Load(ctx, ExplicitConfigFile(path))
	require.NoError(t, err)

	// In-memory migration: converted view, file untouched, no backup.
	assert.Equal(t, "dev", cfg.Contexts["dev"].Stack)
	assert.Equal(t, "https://devstack.grafana.net", cfg.Contexts["dev"].Grafana.Server)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(onDisk))
	_, err = os.Stat(path + legacyBackupSuffix)
	assert.True(t, os.IsNotExist(err))
	assert.Contains(t, warnings.String(), "Warning: running with in-memory config migration")
	assert.Contains(t, warnings.String(), path)
	assert.Contains(t, warnings.String(), "Config and credential writes remain blocked")
	assert.Contains(t, warnings.String(), "gcx will try again on each invocation")
}

func TestMigrateLegacyConfigBackupIsWriteOnce(t *testing.T) {
	withFakeKeychain(t)
	path := writeTestConfig(t, `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
current-context: dev
`)
	existingBackup := `
contexts:
  rollback:
    grafana:
      server: https://rollback.example
current-context: rollback
`
	require.NoError(t, os.WriteFile(path+legacyBackupSuffix, []byte(existingBackup), 0o600))

	_, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.Equal(t, existingBackup, string(backup))

	// A stale rollback file cannot authorize replacing a different current
	// source: both files remain untouched for manual recovery.
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, isLegacyConfig(rewritten))
}

func TestMigrateLegacyConfigInvalidExistingBackupLeavesSourceUntouched(t *testing.T) {
	withFakeKeychain(t)
	legacy := `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
current-context: dev
`
	path := writeTestConfig(t, legacy)
	existingBackup := "# incomplete backup\n"
	require.NoError(t, os.WriteFile(path+legacyBackupSuffix, []byte(existingBackup), 0o600))

	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(context.Background(), &warnings)
	cfg, err := Load(ctx, ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, "https://devstack.grafana.net", cfg.Contexts["dev"].Grafana.Server)

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(onDisk), "invalid rollback safety must prevent persistence")
	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.Equal(t, existingBackup, string(backup))
	assert.Contains(t, warnings.String(), "existing legacy config backup does not match the current source")
}

func TestMigrateLegacyConfigPersistFailureWarnsUnconditionally(t *testing.T) {
	withFakeKeychain(t)
	path := writeTestConfig(t, `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
current-context: dev
`)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	originalRename := renameConfigFile
	renameConfigFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameConfigFile = originalRename })

	var warnings bytes.Buffer
	ctx := ContextWithWarningWriter(context.Background(), &warnings)
	cfg, err := Load(ctx, ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.True(t, cfg.migrationDeferred)
	assert.Contains(t, warnings.String(), "Warning: running with in-memory config migration")
	assert.Contains(t, warnings.String(), "injected rename failure")
	assert.Equal(t, 1, strings.Count(warnings.String(), "Warning:"), "the command diagnostic and structured logger must not duplicate the warning")
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
}

func TestMigrateLegacyConfigValidationEquivalence(t *testing.T) {
	// A context that fails validation before migration fails identically
	// after: migration is structural and must not gate on semantic validity.
	withFakeKeychain(t)
	path := writeTestConfig(t, `
contexts:
  broken:
    grafana:
      oauth-token: gat_partial
current-context: broken
`)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	err = cfg.Contexts["broken"].Validate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server is required")
}

func TestMigrateLegacyConfigEmptyContexts(t *testing.T) {
	withFakeKeychain(t)
	path := writeTestConfig(t, `
contexts:
  empty: {}
  configured:
    grafana:
      server: https://devstack.grafana.net
current-context: configured
`)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	require.NotNil(t, cfg.Contexts["empty"])
	assert.Empty(t, cfg.Contexts["empty"].Stack)
	assert.Empty(t, cfg.Contexts["empty"].Cloud)
	assert.Equal(t, "configured", cfg.Contexts["configured"].Stack)
}

func TestCloudEntryName(t *testing.T) {
	tests := []struct {
		apiURL string
		want   string
	}{
		{"", "grafana-com"},
		{"https://grafana.com", "grafana-com"},
		{"https://grafana-ops.com", "grafana-ops-com"},
		{"grafana-dev.com", "grafana-dev-com"},
		{"://bad url", "grafana-com"},
	}
	for _, tc := range tests {
		t.Run(tc.apiURL, func(t *testing.T) {
			assert.Equal(t, tc.want, cloudEntryName(tc.apiURL))
		})
	}
}

func TestMigrateLegacyLayeredConfigs(t *testing.T) {
	// Same-named context split across layers: user layer has grafana, local
	// layer overlays cloud auth. Each layer migrates independently; the local
	// layer's entry is qualified so it cannot shadow a user-layer entry.
	withFakeKeychain(t)
	userDir := t.TempDir()
	workDir := t.TempDir()
	userFile := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o700))
	require.NoError(t, os.WriteFile(userFile, []byte(`
contexts:
  prod:
    grafana:
      server: https://prodstack.grafana.net
    cloud:
      token: user-token
current-context: prod
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, LocalConfigFileName), []byte(`
contexts:
  prod:
    cloud:
      token: local-token
`), 0o600))

	sources, err := DiscoverSources(WithSystemDir(filepath.Join(userDir, "no-system")), WithUserDir(userDir), WithWorkDir(workDir))
	require.NoError(t, err)
	require.Len(t, sources, 2)

	var merged Config
	for i, src := range sources {
		loaded, err := Load(withConfigLayer(context.Background(), src.Type), ExplicitConfigFile(src.Path))
		require.NoError(t, err)
		if i == 0 {
			merged = loaded
		} else {
			merged = MergeConfigs(merged, loaded)
		}
	}

	// Both entries exist under distinct names; the local layer's context ref
	// wins wholesale, so prod resolves the local token, and the user-layer
	// entry is untouched.
	prod := merged.Contexts["prod"]
	require.NotNil(t, prod)
	assert.Equal(t, "https://prodstack.grafana.net", prod.Grafana.Server)
	require.NotNil(t, prod.CloudEntry)
	assert.Equal(t, "local-token", prod.CloudEntry.Token)
	assert.Equal(t, "grafana-com-local", prod.Cloud)
	require.NotNil(t, merged.Cloud["grafana-com"])
	assert.Equal(t, "user-token", merged.Cloud["grafana-com"].Token)
}

func TestMigratedConfigMergesWithNewFormatLayer(t *testing.T) {
	// A legacy layer merged with a new-format layer during the transition.
	withFakeKeychain(t)
	legacyPath := writeTestConfig(t, `
contexts:
  prod:
    grafana:
      server: https://prodstack.grafana.net
current-context: prod
`)
	newPath := writeTestConfig(t, `
version: 1
stacks:
  extra:
    grafana:
      server: https://extra.grafana.net
contexts:
  extra:
    stack: extra
`)

	base, err := Load(context.Background(), ExplicitConfigFile(legacyPath))
	require.NoError(t, err)
	over, err := Load(context.Background(), ExplicitConfigFile(newPath))
	require.NoError(t, err)

	merged := MergeConfigs(base, over)
	assert.Equal(t, "https://prodstack.grafana.net", merged.Contexts["prod"].Grafana.Server)
	assert.Equal(t, "https://extra.grafana.net", merged.Contexts["extra"].Grafana.Server)
	assert.Equal(t, int64(ConfigVersion), merged.Version)
}

func TestMigrateLegacyConfigDatasourceMapPrecedence(t *testing.T) {
	withFakeKeychain(t)
	path := writeTestConfig(t, `
contexts:
  dev:
    datasources:
      prometheus: map-prom
    default-prometheus-datasource: legacy-prom
    default-tempo-datasource: legacy-tempo
current-context: dev
`)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	dev := cfg.Contexts["dev"]
	assert.Equal(t, "map-prom", DefaultDatasourceUID(*dev, "prometheus"))
	assert.Equal(t, "legacy-tempo", DefaultDatasourceUID(*dev, "tempo"))
}

func TestReadDiagnosticsFromLegacyConfig(t *testing.T) {
	// The diagnostics pre-read runs before the main load on every command; it
	// must honour settings in a not-yet-migrated file (telemetry opt-out on
	// the first run after upgrading) without migrating or writing anything.
	legacy := `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
current-context: dev
diagnostics:
  telemetry: disabled
`
	path := writeTestConfig(t, legacy)

	d, err := readDiagnostics(path)
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, "disabled", d.Telemetry)

	// Read-only: no migration, no backup.
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(onDisk))
	_, err = os.Stat(path + legacyBackupSuffix)
	assert.True(t, os.IsNotExist(err))
}

func TestMergeConfigsEntriesAreAtomicAcrossLayers(t *testing.T) {
	// A repo-local layer must not be able to graft its own destination onto
	// an entry whose credential lives in the user layer: same-named stack and
	// cloud entries replace wholesale, never merge field-by-field.
	base := Config{
		Stacks: map[string]*StackConfig{
			"prod": {Grafana: &GrafanaConfig{Server: "https://prodstack.grafana.net", APIToken: "user-grafana-token"}},
		},
		Cloud: map[string]*CloudEntry{
			"grafana-com": {Token: "user-cloud-token"},
		},
		Contexts: map[string]*Context{
			"prod": {Stack: "prod", Cloud: "grafana-com"},
		},
		CurrentContext: "prod",
	}
	over := Config{
		Stacks: map[string]*StackConfig{
			"prod": {Grafana: &GrafanaConfig{Server: "https://evil.example.com"}},
		},
		Cloud: map[string]*CloudEntry{
			"grafana-com": {APIUrl: "https://evil.example.com"},
		},
	}

	merged := MergeConfigs(base, over)

	prod := merged.Contexts["prod"]
	require.NotNil(t, prod)
	require.NotNil(t, prod.Grafana)
	assert.Equal(t, "https://evil.example.com", prod.Grafana.Server)
	assert.Empty(t, prod.Grafana.APIToken, "user credential must not travel to a layer-supplied destination")
	require.NotNil(t, prod.CloudEntry)
	assert.Equal(t, "https://evil.example.com", prod.CloudEntry.APIUrl)
	assert.Empty(t, prod.CloudEntry.Token, "user cloud token must not travel to a layer-supplied destination")
}

func TestLoadLayeredMigratesEachLayer(t *testing.T) {
	withFakeKeychain(t)
	userDir := t.TempDir()
	userFile := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o700))
	require.NoError(t, os.WriteFile(userFile, []byte(`
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
current-context: dev
`), 0o600))

	t.Setenv(ConfigFileEnvVar, userFile)
	cfg, err := LoadLayered(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "dev", cfg.Contexts["dev"].Stack)

	rewritten, err := os.ReadFile(userFile)
	require.NoError(t, err)
	assert.False(t, isLegacyConfig(rewritten))
	assert.Contains(t, string(rewritten), "stacks:")
}
