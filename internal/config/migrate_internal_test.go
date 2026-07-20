package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKeychain is an in-memory credentials.Store for migration tests.
type fakeKeychain struct {
	entries map[string]string
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{entries: map[string]string{}}
}

func (f *fakeKeychain) Get(key string) (string, error) {
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

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
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
	path := writeTestConfig(t, `
contexts:
  alpha:
    cloud:
      token: token-a
  beta:
    cloud:
      token: token-b
current-context: alpha
`)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
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
	path := writeTestConfig(t, `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
      token: keychain:gcx:dev:grafana-token
    cloud:
      token: keychain:gcx:dev:cloud-token
current-context: dev
`)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	// In-memory view holds resolved plaintext.
	assert.Equal(t, "secret-cloud", cfg.Contexts["dev"].CloudEntry.Token)
	assert.Equal(t, "secret-grafana", cfg.Contexts["dev"].Grafana.APIToken)

	// Values were copied to the canonical owner keys; the legacy keys survive
	// so the .legacy.bak sentinels stay resolvable.
	assert.Equal(t, "secret-cloud", store.entries["cloud:grafana-com:cloud-token"])
	assert.Equal(t, "secret-grafana", store.entries["stack:dev:grafana-token"])
	assert.Equal(t, "secret-cloud", store.entries["dev:cloud-token"])
	assert.Equal(t, "secret-grafana", store.entries["dev:grafana-token"])

	// The rewritten file references the canonical keys.
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(rewritten), "keychain:gcx:cloud:grafana-com:cloud-token")
	assert.Contains(t, string(rewritten), "keychain:gcx:stack:dev:grafana-token")
}

func TestMigrateLegacyConfigPlaintextSecretsNotInBackup(t *testing.T) {
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

	// Plaintext was sentinelized under the legacy key before the backup was
	// taken, so the backup carries a sentinel, not the secret.
	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.NotContains(t, string(backup), "plaintext-secret")
	assert.Contains(t, string(backup), "keychain:gcx:dev:grafana-token")
	assert.Equal(t, "plaintext-secret", store.entries["dev:grafana-token"])

	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(rewritten), "plaintext-secret")
}

func TestMigrateLegacyConfigKeychainUnavailable(t *testing.T) {
	// The default test store reports ErrUnavailable for every operation,
	// mirroring a headless box. Migration must still work: plaintext carries
	// through, unresolvable sentinels are carried verbatim.
	path := writeTestConfig(t, `
contexts:
  dev:
    grafana:
      server: https://devstack.grafana.net
      token: plaintext-secret
    cloud:
      token: keychain:gcx:dev:cloud-token
current-context: dev
`)

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	assert.Equal(t, "plaintext-secret", cfg.Contexts["dev"].Grafana.APIToken)

	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	// Plaintext stays plaintext (no keychain to move it into) and the legacy
	// sentinel is preserved verbatim in its new home.
	assert.Contains(t, string(rewritten), "plaintext-secret")
	assert.Contains(t, string(rewritten), "keychain:gcx:dev:cloud-token")

	// The carried legacy sentinel resolves once the keychain comes back.
	store := withFakeKeychain(t)
	store.entries["dev:cloud-token"] = "recovered"
	recovered, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)
	assert.Equal(t, "recovered", recovered.Contexts["dev"].CloudEntry.Token)
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

	cfg, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	// In-memory migration: converted view, file untouched, no backup.
	assert.Equal(t, "dev", cfg.Contexts["dev"].Stack)
	assert.Equal(t, "https://devstack.grafana.net", cfg.Contexts["dev"].Grafana.Server)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(onDisk))
	_, err = os.Stat(path + legacyBackupSuffix)
	assert.True(t, os.IsNotExist(err))
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
	existingBackup := "# pre-existing backup\n"
	require.NoError(t, os.WriteFile(path+legacyBackupSuffix, []byte(existingBackup), 0o600))

	_, err := Load(context.Background(), ExplicitConfigFile(path))
	require.NoError(t, err)

	backup, err := os.ReadFile(path + legacyBackupSuffix)
	require.NoError(t, err)
	assert.Equal(t, existingBackup, string(backup))

	// Migration still persisted despite skipping the backup write.
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.False(t, isLegacyConfig(rewritten))
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
