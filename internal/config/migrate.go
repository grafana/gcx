package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/gofrs/flock"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/grafana-app-sdk/logging"
)

// legacyBackupSuffix is appended to the config filename when the loader backs
// up a legacy-format file before migrating it. The backup is a write-once,
// byte-for-byte 0600 copy, so restoring it fully rolls back the migration.
const legacyBackupSuffix = ".legacy.bak"

// legacyConfig mirrors the pre-versioned config format, where every context
// carried its own grafana connection, cloud credentials, and provider config.
type legacyConfig struct {
	Contexts       map[string]*legacyContext `yaml:"contexts"`
	CurrentContext string                    `yaml:"current-context"`
	Diagnostics    *DiagnosticsConfig        `yaml:"diagnostics,omitempty"`
}

type legacyContext struct {
	Grafana                     *GrafanaConfig               `yaml:"grafana,omitempty"`
	Cloud                       *legacyCloudConfig           `yaml:"cloud,omitempty"`
	DefaultPrometheusDatasource string                       `yaml:"default-prometheus-datasource,omitempty"`
	DefaultLokiDatasource       string                       `yaml:"default-loki-datasource,omitempty"`
	DefaultPyroscopeDatasource  string                       `yaml:"default-pyroscope-datasource,omitempty"`
	DefaultTempoDatasource      string                       `yaml:"default-tempo-datasource,omitempty"`
	Datasources                 map[string]string            `yaml:"datasources,omitempty"`
	Providers                   map[string]map[string]string `yaml:"providers,omitempty"`
	Resources                   *ResourcesConfig             `yaml:"resources,omitempty"`
}

type legacyCloudConfig struct {
	Token    string `yaml:"token,omitempty"`
	Stack    string `yaml:"stack,omitempty"`
	OAuthUrl string `yaml:"oauth-url,omitempty"`
	APIUrl   string `yaml:"api-url,omitempty"`
}

// legacyContextKeys are the context-level keys that only exist in the legacy
// format (in the current format they live on stack entries, or were dropped).
//
//nolint:gochecknoglobals // constant-like lookup list; never mutated.
var legacyContextKeys = []string{
	"grafana",
	"providers",
	"resources",
	"default-prometheus-datasource",
	"default-loki-datasource",
	"default-pyroscope-datasource",
	"default-tempo-datasource",
}

// isLegacyConfig reports whether raw config bytes are in the legacy
// (pre-versioned) format. Detection is by shape: a `version` field or
// top-level `stacks`/`cloud` maps mark the current format; a context carrying
// legacy-only keys (or a `cloud` mapping rather than a name reference) marks
// the legacy one. Configs that decode identically under both formats (e.g.
// the default empty config) are treated as current. Unparseable input returns
// false so the strict decoder surfaces the real error.
func isLegacyConfig(contents []byte) bool {
	var raw map[string]any
	if err := yaml.Unmarshal(contents, &raw); err != nil {
		return false
	}
	if _, ok := raw["version"]; ok {
		return false
	}
	if _, ok := raw["stacks"]; ok {
		return false
	}
	if _, ok := raw["cloud"]; ok {
		return false
	}
	contexts, ok := raw["contexts"].(map[string]any)
	if !ok {
		return false
	}
	for _, v := range contexts {
		ctx, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range legacyContextKeys {
			if _, ok := ctx[key]; ok {
				return true
			}
		}
		if cloud, ok := ctx["cloud"]; ok {
			if _, isMap := cloud.(map[string]any); isMap {
				return true
			}
		}
	}
	return false
}

// legacySecretKey identifies one secret field on one legacy context.
type legacySecretKey struct {
	context string
	field   credentials.Field
}

// legacySecretRef returns a get/set handle for the named secret on a legacy
// context, or ok=false when the field's parent is absent.
func legacySecretRef(lctx *legacyContext, field credentials.Field) (secretRef, bool) {
	switch field {
	case credentials.FieldCloudToken:
		if lctx.Cloud == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return lctx.Cloud.Token },
			set: func(v string) { lctx.Cloud.Token = v },
		}, true
	case credentials.FieldGrafanaToken:
		if lctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return lctx.Grafana.APIToken },
			set: func(v string) { lctx.Grafana.APIToken = v },
		}, true
	case credentials.FieldGrafanaPassword:
		if lctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return lctx.Grafana.Password },
			set: func(v string) { lctx.Grafana.Password = v },
		}, true
	case credentials.FieldOAuthToken:
		if lctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return lctx.Grafana.OAuthToken },
			set: func(v string) { lctx.Grafana.OAuthToken = v },
		}, true
	case credentials.FieldOAuthRefreshToken:
		if lctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return lctx.Grafana.OAuthRefreshToken },
			set: func(v string) { lctx.Grafana.OAuthRefreshToken = v },
		}, true
	case credentials.FieldSMToken:
		if lctx.Providers == nil || lctx.Providers["synth"] == nil {
			return secretRef{}, false
		}
		if _, ok := lctx.Providers["synth"]["sm-token"]; !ok {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return lctx.Providers["synth"]["sm-token"] },
			set: func(v string) { lctx.Providers["synth"]["sm-token"] = v },
		}, true
	}
	return secretRef{}, false
}

// collectLegacySecrets resolves every secret in a legacy config to plaintext
// where possible. Plaintext fields are always copied verbatim. Unbound legacy
// sentinels are resolved only for a trusted migration source and only after the
// reference matches its containing context and exact schema field. Repository-
// local config is not trusted to select a process-global legacy account.
func collectLegacySecrets(lc *legacyConfig, store credentials.Store, allowLegacyGet bool) (map[legacySecretKey]string, bool, error) {
	resolved := map[legacySecretKey]string{}
	transientFailure := false
	for name, lctx := range lc.Contexts {
		if lctx == nil {
			continue
		}
		for _, field := range credentials.AllFields {
			ref, ok := legacySecretRef(lctx, field)
			if !ok {
				continue
			}
			cur := ref.get()
			if cur == "" {
				continue
			}
			if !credentials.IsSentinel(cur) {
				resolved[legacySecretKey{name, field}] = cur
				continue
			}
			if !allowLegacyGet {
				return nil, false, fmt.Errorf(
					"legacy keychain reference for context %q field %q cannot be auto-migrated from an untrusted config source; no config files or credentials were changed; replace the reference with a credential or migrate it from the user config (%s)",
					name, field, docs.ConfigMigration,
				)
			}
			// Legacy references are unbound, so the containing context and exact
			// schema field — not values embedded in YAML — are the authority for
			// the one-time migration lookup.
			value, err := resolveLegacySentinelForMigration(cur, name, field, store)
			switch {
			case err == nil:
				resolved[legacySecretKey{name, field}] = value
			case errors.Is(err, credentials.ErrNotFound):
				// A reachable keychain proved this exact legacy account is gone, but
				// the sentinel remains durable evidence that authentication was
				// configured. Preserve it verbatim through conversion so the v1
				// resolver quarantines it and consumers fail before falling through
				// to anonymous or a lower-priority authentication method. Raw edit or
				// re-authentication can then repair it explicitly.
				continue
			case errors.Is(err, credentials.ErrUnavailable):
				transientFailure = true
			case errors.Is(err, errLegacySentinelMismatch):
				return nil, false, fmt.Errorf(
					"invalid legacy keychain reference for context %q field %q: %w; no config files or credentials were changed (%s)",
					name, field, err, docs.ConfigMigration,
				)
			default:
				// Keychain backends can return platform-specific transient errors.
				// Keep the legacy source untouched and retry on the next invocation.
				transientFailure = true
			}
		}
	}
	return resolved, transientFailure, nil
}

// cloudEntryName derives a cloud entry name from a GCOM API URL host
// ("grafana-com", "grafana-ops-com"). The host describes the cloud
// environment, which is what an entry models; org-slug naming needs a network
// call migration cannot make.
func cloudEntryName(apiURL string) string {
	if apiURL == "" {
		return "grafana-com"
	}
	parsed, err := url.Parse(NormalizeCloudURL(apiURL))
	if err != nil || parsed.Hostname() == "" {
		return "grafana-com"
	}
	return strings.ReplaceAll(parsed.Hostname(), ".", "-")
}

// layerEntrySuffix qualifies migrated cloud entry names by config layer, so
// entries minted independently in two layers cannot shadow each other after
// the layered merge (same-named contexts overlaid across layers still merge,
// because the higher layer's context ref wins wholesale).
func layerEntrySuffix(layerType string) string {
	switch layerType {
	case "local", "system":
		return "-" + layerType
	default:
		return ""
	}
}

// convertLegacyConfig transforms a legacy config into the current format:
// each context becomes a same-named stack entry (1:1, no dedup — grafana
// credentials are genuinely per-context) plus a context referencing it; cloud
// credentials become named entries deduplicated across contexts by resolved
// (token, api-url, oauth-url) tuple. Secrets resolve to plaintext where the
// keychain allows (Write re-keys them under the new owner keys); unresolvable
// sentinels are carried verbatim and keep resolving via their embedded legacy
// account keys.
func convertLegacyConfig(lc *legacyConfig, layerType string, secrets map[legacySecretKey]string) *Config {
	cfg := &Config{
		Version:        ConfigVersion,
		CurrentContext: lc.CurrentContext,
		Diagnostics:    cloneLegacyDiagnostics(lc.Diagnostics),
	}

	secretValue := func(ctxName string, field credentials.Field, raw string) string {
		if value, ok := secrets[legacySecretKey{ctxName, field}]; ok {
			return value
		}
		return raw
	}

	names := make([]string, 0, len(lc.Contexts))
	for name := range lc.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	entryNames := map[string]string{} // dedup key → entry name
	suffix := layerEntrySuffix(layerType)

	for _, name := range names {
		lctx := lc.Contexts[name]
		newCtx := &Context{}
		if cfg.Contexts == nil {
			cfg.Contexts = map[string]*Context{}
		}
		cfg.Contexts[name] = newCtx
		if lctx == nil {
			continue
		}

		if stack := convertLegacyStack(name, lctx, secretValue); stack != nil {
			if cfg.Stacks == nil {
				cfg.Stacks = map[string]*StackConfig{}
			}
			cfg.Stacks[name] = stack
			newCtx.Stack = name
		}

		newCtx.Cloud = ensureLegacyCloudEntry(cfg, name, lctx, suffix, entryNames, secretValue)
		newCtx.Datasources = mergeLegacyDatasources(lctx)
	}

	cfg.Resolve()
	return cfg
}

// convertLegacyStack builds the stack entry for one legacy context, resolving
// its secrets to plaintext where possible. Returns nil when the context has
// nothing stack-shaped (cloud-auth-only or empty contexts).
func convertLegacyStack(name string, lctx *legacyContext, secretValue func(string, credentials.Field, string) string) *StackConfig {
	var slug string
	if lctx.Cloud != nil {
		slug = lctx.Cloud.Stack
	}
	if lctx.Grafana == nil && len(lctx.Providers) == 0 && lctx.Resources == nil && slug == "" {
		return nil
	}

	stack := &StackConfig{
		Slug:      slug,
		Grafana:   cloneLegacyGrafana(lctx.Grafana),
		Providers: cloneLegacyProviders(lctx.Providers),
		Resources: cloneLegacyResources(lctx.Resources),
	}
	if stack.Grafana != nil {
		stack.Grafana.APIToken = secretValue(name, credentials.FieldGrafanaToken, stack.Grafana.APIToken)
		stack.Grafana.Password = secretValue(name, credentials.FieldGrafanaPassword, stack.Grafana.Password)
		stack.Grafana.OAuthToken = secretValue(name, credentials.FieldOAuthToken, stack.Grafana.OAuthToken)
		stack.Grafana.OAuthRefreshToken = secretValue(name, credentials.FieldOAuthRefreshToken, stack.Grafana.OAuthRefreshToken)
	}
	if stack.Providers != nil && stack.Providers["synth"] != nil {
		if raw, ok := stack.Providers["synth"]["sm-token"]; ok {
			stack.Providers["synth"]["sm-token"] = secretValue(name, credentials.FieldSMToken, raw)
		}
	}
	return stack
}

func cloneLegacyDiagnostics(in *DiagnosticsConfig) *DiagnosticsConfig {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneLegacyGrafana(in *GrafanaConfig) *GrafanaConfig {
	if in == nil {
		return nil
	}
	out := *in
	if in.TLS != nil {
		tlsCopy := *in.TLS
		tlsCopy.CertData = slices.Clone(in.TLS.CertData)
		tlsCopy.KeyData = slices.Clone(in.TLS.KeyData)
		tlsCopy.CAData = slices.Clone(in.TLS.CAData)
		tlsCopy.NextProtos = slices.Clone(in.TLS.NextProtos)
		tlsCopy.credentialCertFile.contents = slices.Clone(in.TLS.credentialCertFile.contents)
		tlsCopy.credentialKeyFile.contents = slices.Clone(in.TLS.credentialKeyFile.contents)
		tlsCopy.credentialCAFile.contents = slices.Clone(in.TLS.credentialCAFile.contents)
		out.TLS = &tlsCopy
	}
	return &out
}

func cloneLegacyProviders(in map[string]map[string]string) map[string]map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(in))
	for provider, values := range in {
		if values == nil {
			out[provider] = nil
			continue
		}
		valuesCopy := make(map[string]string, len(values))
		maps.Copy(valuesCopy, values)
		out[provider] = valuesCopy
	}
	return out
}

func cloneLegacyResources(in *ResourcesConfig) *ResourcesConfig {
	if in == nil {
		return nil
	}
	out := *in
	out.AssumeServerDryRun = slices.Clone(in.AssumeServerDryRun)
	return &out
}

// ensureLegacyCloudEntry converts one legacy context's cloud credentials into
// a named entry, reusing an existing entry when the resolved (token, api-url,
// oauth-url) tuple matches one already minted (dedup). Returns the entry name
// to bind, or "" when the context carries no cloud credentials.
func ensureLegacyCloudEntry(cfg *Config, name string, lctx *legacyContext, suffix string, entryNames map[string]string, secretValue func(string, credentials.Field, string) string) string {
	if lctx.Cloud == nil || (lctx.Cloud.Token == "" && lctx.Cloud.APIUrl == "" && lctx.Cloud.OAuthUrl == "") {
		return ""
	}
	token := secretValue(name, credentials.FieldCloudToken, lctx.Cloud.Token)
	apiURL := lctx.Cloud.APIUrl
	if apiURL == "" && lctx.Cloud.OAuthUrl != "" {
		apiURL = lctx.Cloud.OAuthUrl
	}
	if apiURL == "" && lctx.Grafana != nil {
		apiURL, _ = GCOMRootFromServerURL(lctx.Grafana.Server)
	}
	if apiURL == "" {
		apiURL = "https://grafana.com"
	}
	oauthURL := lctx.Cloud.OAuthUrl
	if oauthURL == "" {
		oauthURL = apiURL
	}
	dedupKey := token + "\x00" + apiURL + "\x00" + oauthURL
	if entryName, ok := entryNames[dedupKey]; ok {
		return entryName
	}

	entryName := cloudEntryName(apiURL) + suffix
	if _, taken := cfg.Cloud[entryName]; taken {
		entryName += "-" + name
	}
	if cfg.Cloud == nil {
		cfg.Cloud = map[string]*CloudEntry{}
	}
	cfg.Cloud[entryName] = &CloudEntry{
		Token:    token,
		APIUrl:   apiURL,
		OAuthUrl: oauthURL,
	}
	entryNames[dedupKey] = entryName
	return entryName
}

// mergeLegacyDatasources folds the legacy default-*-datasource fields into the
// per-kind datasources map. The map took precedence over the flat fields, so
// existing keys win.
func mergeLegacyDatasources(lctx *legacyContext) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, lctx.Datasources)
	legacy := map[string]string{
		"prometheus": lctx.DefaultPrometheusDatasource,
		"loki":       lctx.DefaultLokiDatasource,
		"pyroscope":  lctx.DefaultPyroscopeDatasource,
		"tempo":      lctx.DefaultTempoDatasource,
	}
	for kind, uid := range legacy {
		if uid != "" && merged[kind] == "" {
			merged[kind] = uid
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// verifyLegacyMigration checks that a converted config is observably
// equivalent to the legacy config it came from: same contexts, same resolved
// grafana/provider/cloud/datasource/slug/dry-run views. It runs before the
// legacy file is replaced, so a converter bug surfaces as a load error with
// the file untouched.
func verifyLegacyMigration(lc *legacyConfig, cfg *Config, secrets map[legacySecretKey]string) error {
	if lc.CurrentContext != cfg.CurrentContext {
		return fmt.Errorf("current-context: %q != %q", cfg.CurrentContext, lc.CurrentContext)
	}
	if len(lc.Contexts) != len(cfg.Contexts) {
		return fmt.Errorf("context count: %d != %d", len(cfg.Contexts), len(lc.Contexts))
	}
	secretValue := func(ctxName string, field credentials.Field, raw string) string {
		if value, ok := secrets[legacySecretKey{ctxName, field}]; ok {
			return value
		}
		return raw
	}
	for name, lctx := range lc.Contexts {
		newCtx := cfg.Contexts[name]
		if newCtx == nil {
			return fmt.Errorf("context %q missing after conversion", name)
		}
		if lctx == nil {
			continue
		}
		if newCtx.Stack != "" && newCtx.StackEntry == nil {
			return fmt.Errorf("context %q: dangling stack ref %q", name, newCtx.Stack)
		}
		if newCtx.Cloud != "" && newCtx.CloudEntry == nil {
			return fmt.Errorf("context %q: dangling cloud ref %q", name, newCtx.Cloud)
		}
		expectedGrafana := cloneLegacyGrafana(lctx.Grafana)
		if expectedGrafana != nil {
			expectedGrafana.APIToken = secretValue(name, credentials.FieldGrafanaToken, expectedGrafana.APIToken)
			expectedGrafana.Password = secretValue(name, credentials.FieldGrafanaPassword, expectedGrafana.Password)
			expectedGrafana.OAuthToken = secretValue(name, credentials.FieldOAuthToken, expectedGrafana.OAuthToken)
			expectedGrafana.OAuthRefreshToken = secretValue(name, credentials.FieldOAuthRefreshToken, expectedGrafana.OAuthRefreshToken)
		}
		if !reflect.DeepEqual(expectedGrafana, newCtx.Grafana) {
			return fmt.Errorf("context %q: grafana config differs after conversion", name)
		}
		expectedProviders := cloneLegacyProviders(lctx.Providers)
		if synth := expectedProviders["synth"]; synth != nil {
			if raw, ok := synth["sm-token"]; ok {
				synth["sm-token"] = secretValue(name, credentials.FieldSMToken, raw)
			}
		}
		if len(lctx.Providers) > 0 && !reflect.DeepEqual(expectedProviders, newCtx.Providers) {
			return fmt.Errorf("context %q: provider config differs after conversion", name)
		}
		if err := verifyLegacyCloud(name, lctx, newCtx, secretValue); err != nil {
			return err
		}
		for _, kind := range datasourceKindsToVerify(lctx) {
			if got, want := newCtx.Datasources[kind], legacyDatasourceUID(lctx, kind); got != want {
				return fmt.Errorf("context %q: %s datasource %q != %q", name, kind, got, want)
			}
		}
		var expectedDryRun []string
		if lctx.Resources != nil {
			expectedDryRun = lctx.Resources.AssumeServerDryRun
		}
		if !sameStringSet(newCtx.AssumeServerDryRun(), expectedDryRun) {
			return fmt.Errorf("context %q: assume-server-dry-run differs after conversion", name)
		}
	}
	return nil
}

// verifyLegacyCloud checks that one context's converted cloud view (stack
// slug and cloud entry) matches the legacy cloud block it came from.
func verifyLegacyCloud(name string, lctx *legacyContext, newCtx *Context, secretValue func(string, credentials.Field, string) string) error {
	if lctx.Cloud == nil {
		return nil
	}
	if expectedSlug := lctx.Cloud.Stack; expectedSlug != "" && newCtx.ResolveStackSlug() != expectedSlug {
		return fmt.Errorf("context %q: stack slug %q != %q", name, newCtx.ResolveStackSlug(), expectedSlug)
	}
	if lctx.Cloud.Token == "" && lctx.Cloud.APIUrl == "" && lctx.Cloud.OAuthUrl == "" {
		return nil
	}
	entry := newCtx.CloudEntry
	if entry == nil {
		return fmt.Errorf("context %q: cloud credentials missing after conversion", name)
	}
	expectedAPI := lctx.Cloud.APIUrl
	if expectedAPI == "" && lctx.Cloud.OAuthUrl != "" {
		expectedAPI = lctx.Cloud.OAuthUrl
	}
	if expectedAPI == "" && lctx.Grafana != nil {
		expectedAPI, _ = GCOMRootFromServerURL(lctx.Grafana.Server)
	}
	if expectedAPI == "" {
		expectedAPI = "https://grafana.com"
	}
	expectedOAuth := lctx.Cloud.OAuthUrl
	if expectedOAuth == "" {
		expectedOAuth = expectedAPI
	}
	if entry.Token != secretValue(name, credentials.FieldCloudToken, lctx.Cloud.Token) ||
		normalizeCredentialURL(entry.APIUrl, "https://grafana.com") != normalizeCredentialURL(expectedAPI, "https://grafana.com") ||
		normalizeCredentialURL(entry.OAuthUrl, expectedAPI) != normalizeCredentialURL(expectedOAuth, expectedAPI) {
		return fmt.Errorf("context %q: cloud entry differs after conversion", name)
	}
	return nil
}

func datasourceKindsToVerify(lctx *legacyContext) []string {
	kinds := []string{"prometheus", "loki", "pyroscope", "tempo"}
	for kind := range lctx.Datasources {
		switch kind {
		case "prometheus", "loki", "pyroscope", "tempo":
		default:
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// legacyDatasourceUID mirrors the legacy DefaultDatasourceUID resolution: the
// per-kind map wins over the flat default fields.
func legacyDatasourceUID(lctx *legacyContext, kind string) string {
	if uid := lctx.Datasources[kind]; uid != "" {
		return uid
	}
	switch kind {
	case "prometheus":
		return lctx.DefaultPrometheusDatasource
	case "loki":
		return lctx.DefaultLokiDatasource
	case "pyroscope":
		return lctx.DefaultPyroscopeDatasource
	case "tempo":
		return lctx.DefaultTempoDatasource
	}
	return ""
}

func sameStringSet(a, b []string) bool {
	left := make(map[string]struct{}, len(a))
	for _, s := range a {
		left[s] = struct{}{}
	}
	right := make(map[string]struct{}, len(b))
	for _, s := range b {
		right[s] = struct{}{}
	}
	if len(left) != len(right) {
		return false
	}
	for s := range left {
		if _, ok := right[s]; !ok {
			return false
		}
	}
	return true
}

func decodeLegacyMigrationInputs(codec *format.YAMLCodec, contents []byte) (legacyConfig, legacyConfig, error) {
	var input legacyConfig
	if err := codec.Decode(bytes.NewReader(contents), &input); err != nil {
		return legacyConfig{}, legacyConfig{}, err
	}
	var baseline legacyConfig
	if err := codec.Decode(bytes.NewReader(contents), &baseline); err != nil {
		return legacyConfig{}, legacyConfig{}, err
	}
	return input, baseline, nil
}

func verifyLegacyConversion(input, baseline *legacyConfig, cfg *Config, secrets map[legacySecretKey]string) error {
	if !reflect.DeepEqual(input, baseline) {
		return errors.New("conversion mutated the independently decoded legacy input")
	}
	return verifyLegacyMigration(baseline, cfg, secrets)
}

// migrateLegacyConfig converts legacy config bytes to the current format and
// persists the result. It deletes nothing: the legacy file is replaced
// atomically only after a write-once backup exists and the converted config
// passes self-verification, and keychain entries are only ever copied (the
// legacy per-context entries keep the backup restorable). Every persistence
// failure degrades to an in-memory migration with a warning, and the next
// load retries.
func migrateLegacyConfig(ctx context.Context, source Source, filename string, contents []byte) (Config, error) {
	log := logging.FromContext(ctx)
	migrationPath, err := canonicalConfigSourceForLayer(filename, configLayerFromCtx(ctx))
	if err != nil {
		return Config{}, err
	}

	// Serialize migration for this source. The initial legacy-shape check happens
	// before entering this function, so reread under the lock: another process may
	// already have completed the migration while this one was waiting.
	canPersist := !migrationPersistenceSuppressed(ctx)
	var lockErr error
	deferredReason := ""
	if canPersist {
		lockPath, err := configLockFile(migrationPath, "write")
		if err != nil {
			return Config{}, err
		}
		lock := flock.New(lockPath)
		lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var locked bool
		locked, lockErr = lock.TryLockContext(lockCtx, 100*time.Millisecond)
		canPersist = lockErr == nil && locked
		if locked {
			defer func() { _ = lock.Unlock() }()
		}
	}
	if !canPersist {
		reason := layeredMigrationReadOnlyReason
		if !migrationPersistenceSuppressed(ctx) && lockErr == nil {
			reason = "timed out"
		} else if !migrationPersistenceSuppressed(ctx) && lockErr != nil {
			reason = lockErr.Error()
		}
		deferredReason = reason
		log.Debug("config migration persistence unavailable; using in-memory migration",
			"file", filename, "error", reason, "guide", docs.ConfigMigration)
	}

	if canPersist {
		freshContents, err := readConfigSource(ConfigSource{Path: filename, Type: configLayerFromCtx(ctx)})
		if err != nil {
			return Config{}, err
		}
		if snapshot, ok := configSnapshotFromContext(ctx, filename); ok && !bytes.Equal(freshContents, snapshot) {
			return Config{}, fmt.Errorf("config %s changed after migration preflight; no config files or credentials were changed", filename)
		}
		contents = freshContents
	}
	if err := validateDeclaredConfigVersion(filename, contents); err != nil {
		return Config{}, err
	}
	if !isLegacyConfig(contents) {
		var current Config
		codec := &format.YAMLCodec{BytesAsBase64: true}
		if err := codec.Decode(bytes.NewReader(contents), &current); err != nil {
			return Config{}, UnmarshalError{File: filename, Err: err}
		}
		current.Source = filename
		return current, nil
	}

	codec := &format.YAMLCodec{BytesAsBase64: true}
	lc, legacyBaseline, err := decodeLegacyMigrationInputs(codec, contents)
	if err != nil {
		return Config{}, UnmarshalError{File: filename, Err: err}
	}

	store := newLazyStore(keychainStoreFn)
	layerType := configLayerFromCtx(ctx)
	// Auto-discovered repository, system, and arbitrary explicit configs cannot
	// read predictable per-user legacy accounts. Compatibility is limited to the
	// canonical discovered user config with secure write permissions.
	allowLegacyGet := trustedLegacyKeychainSource(ctx, layerType, filename)
	secrets, transientLegacyFailure, err := collectLegacySecrets(&lc, store, allowLegacyGet)
	if err != nil {
		return Config{}, err
	}
	if transientLegacyFailure {
		canPersist = false
		deferredReason = "a legacy credential could not be read from the credential store"
	}

	// The backup is an exact 0600 rollback copy. Plaintext is intentionally not
	// pushed through predictable legacy account names: doing so could overwrite
	// a credential owned by another config source. The converted v1 Write below
	// stores plaintext under source-bound v2 account names.
	backupOK, deferredReason := prepareLegacyBackup(canPersist, deferredReason, filename, contents, codec)

	cfg := convertLegacyConfig(&lc, layerType, secrets)
	cfg.Source = filename
	cfg.migrationDeferred = !backupOK

	// Verification uses a separately decoded, immutable baseline. Conversion
	// must not be able to make its own self-check pass by aliasing and mutating
	// the legacy Grafana/provider/resource nodes it is meant to compare against.
	if err := verifyLegacyConversion(&lc, &legacyBaseline, cfg, secrets); err != nil {
		return Config{}, migrationFailedError("config migration self-check failed", err, filename)
	}

	// Round-trip the converted config through the codec to prove the bytes we
	// are about to persist decode back into an equivalent config.
	var encoded bytes.Buffer
	if err := codec.Encode(&encoded, cfg); err != nil {
		return Config{}, migrationFailedError("config migration failed to encode the converted config", err, filename)
	}
	var back Config
	if err := codec.Decode(bytes.NewReader(encoded.Bytes()), &back); err != nil {
		return Config{}, migrationFailedError("config migration produced an unreadable config", err, filename)
	}
	back.Resolve()
	if err := verifyLegacyMigration(&legacyBaseline, &back, secrets); err != nil {
		return Config{}, migrationFailedError("config migration round-trip check failed", err, filename)
	}

	if !backupOK {
		warnInMemoryMigration(ctx, filename, deferredReason)
		return *cfg, nil
	}

	// Pre-initialize the keychain bookkeeping map so reconcileKeychain's marks
	// (made on Write's copy of the struct) land in a map shared with our
	// return value — the caller's plaintext-migration pass then skips fields
	// this Write already re-keyed.
	cfg.keychainFields = keychainBacked{}
	cfg.migrationDeferred = false
	cfg.sourceLayer = layerType
	cfg.bindSourceIdentity(migrationPath)
	cfg.sourceRevision = sha256.Sum256(contents)
	cfg.hasSourceRevision = true
	if err := Write(withConfigWriteLockHeld(ctx), source, *cfg); err != nil {
		var durabilityErr *configDurabilityError
		if errors.As(err, &durabilityErr) {
			log.Warn("migrated config was replaced but its directory durability barrier failed; old and new keychain generations were retained",
				"file", filename,
				"error", err.Error(),
				"guide", docs.ConfigMigration)
			return *cfg, nil
		}
		cfg.migrationDeferred = true
		warnInMemoryMigration(ctx, filename, err.Error())
		return *cfg, nil
	}

	log.Info("migrated config to the current format", "file", filename, "backup", filename+legacyBackupSuffix)
	if !agent.IsAgentMode() {
		fmt.Fprintf(os.Stderr, "Your gcx config file %s has been migrated to the new v1 format, with a backup of the old file at %s.\nRead about what changed here: %s\n",
			filename, filename+legacyBackupSuffix, docs.HumanURL(docs.ConfigMigration))
	}
	return *cfg, nil
}

func warnInMemoryMigration(ctx context.Context, filename, reason string) {
	if collector := inMemoryMigrationWarningCollectorFromContext(ctx); collector != nil {
		collector.add(filename, reason)
		return
	}
	const message = "running with in-memory config migration: gcx attempted to migrate your config file, but it encountered an issue. The config file was not modified. Config and credential writes remain blocked until migration can be persisted. gcx will try again on each invocation until it succeeds."
	if writer := warningWriterFromCtx(ctx); writer != nil {
		fmt.Fprintf(writer, "Warning: %s: %s; reason: %s (%s)\n", message, filename, reason, docs.ConfigMigration)
		return
	}
	logging.FromContext(ctx).Warn(message, "file", filename, "error", reason, "guide", docs.ConfigMigration)
}

func prepareLegacyBackup(canPersist bool, deferredReason, filename string, contents []byte, codec *format.YAMLCodec) (bool, string) {
	if !canPersist {
		return false, deferredReason
	}
	backupOK, err := writeLegacyBackup(filename, contents, codec)
	if err != nil {
		return false, err.Error()
	}
	return backupOK, deferredReason
}

func trustedLegacyKeychainSource(ctx context.Context, layerType, filename string) bool {
	if layerType == "explicit" {
		canonical, ok := secureLegacyConfigIdentity(filename)
		if !ok {
			return false
		}
		consent, consented := ctx.Value(explicitLegacyMigrationConsentKey{}).(explicitLegacyMigrationConsent)
		return consented && consent.sourceIdentity == canonical
	}
	return layerType == "user" && trustedDiscoveredUserLegacySource(filename)
}

func trustedDiscoveredUserLegacySource(filename string) bool {
	canonical, ok := secureLegacyConfigIdentity(filename)
	if !ok {
		return false
	}
	// A discovered user source is trusted only when its resolved identity is one
	// of the standard user locations. Resolve both sides so a symlinked HOME,
	// XDG root, or config path does not strand credentials owned by that source.
	for _, dir := range userConfigDirs() {
		expected, err := canonicalConfigSource(userConfigFile(dir))
		if err == nil && expected == canonical {
			return true
		}
	}
	return false
}

// secureLegacyConfigIdentity verifies the selected target rather than trusting
// a lexical path. Legacy keychain account names are predictable, so the file
// must be a stable regular file, owned by the current user where the platform
// exposes ownership, and not writable by group or others.
func secureLegacyConfigIdentity(filename string) (string, bool) {
	file, err := os.Open(filename)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm()&0o022 != 0 || !legacyConfigOwnedByCurrentUser(opened) {
		return "", false
	}
	current, err := os.Stat(filename)
	if err != nil || !os.SameFile(opened, current) {
		return "", false
	}
	canonical, err := canonicalConfigSource(filename)
	if err != nil {
		return "", false
	}
	canonicalInfo, err := os.Stat(canonical)
	if err != nil || !os.SameFile(opened, canonicalInfo) {
		return "", false
	}
	return canonical, true
}

// legacyConfigOwnedByCurrentUser checks Unix-style FileInfo ownership when the
// platform exposes a numeric Uid field. Platforms without that metadata retain
// the regular-file and permission checks above.
func legacyConfigOwnedByCurrentUser(info os.FileInfo) bool {
	stat := reflect.ValueOf(info.Sys())
	if !stat.IsValid() {
		return true
	}
	if stat.Kind() == reflect.Pointer {
		if stat.IsNil() {
			return true
		}
		stat = stat.Elem()
	}
	if stat.Kind() != reflect.Struct {
		return true
	}
	uid := stat.FieldByName("Uid")
	if !uid.IsValid() || !uid.CanUint() {
		return true
	}
	current, err := user.Current()
	if err != nil {
		return false
	}
	return current.Uid == strconv.FormatUint(uid.Uint(), 10)
}

// migrationFailedError wraps a migration failure with the two things the user
// needs: confidence that their file was not modified, and the manual
// migration guide.
func migrationFailedError(summary string, err error, filename string) error {
	return fmt.Errorf(
		"%s (%w); the legacy config file %s was left untouched — migrate it manually (%s) and report this at https://github.com/grafana/gcx/issues",
		summary, err, filename, docs.ConfigMigration)
}

// configLayerKey carries the config layer type ("system", "user", "local")
// through context, set by LoadLayered/LoadForWrite when loading a discovered
// layer. Migration reads it to qualify cloud entry names per layer.
type configLayerKey struct{}

type explicitLegacyMigrationConsentKey struct{}

type explicitLegacyMigrationConsent struct {
	sourceIdentity string
}

type migrationPersistenceKey struct{}

const layeredMigrationReadOnlyReason = "layered migration is read-only; migrate each layer explicitly"

type inMemoryMigrationWarning struct {
	filename string
	reason   string
}

// inMemoryMigrationWarningCollector lets a layered load collapse its nested
// per-source diagnostics into one warning without discarding an exceptional
// blocker discovered while producing a source's in-memory view.
type inMemoryMigrationWarningCollector struct {
	mu       sync.Mutex
	warnings []inMemoryMigrationWarning
}

func (c *inMemoryMigrationWarningCollector) add(filename, reason string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warnings = append(c.warnings, inMemoryMigrationWarning{filename: filename, reason: reason})
}

func (c *inMemoryMigrationWarningCollector) exceptionalWarnings() []inMemoryMigrationWarning {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	warnings := make([]inMemoryMigrationWarning, 0, len(c.warnings))
	for _, warning := range c.warnings {
		if warning.reason != layeredMigrationReadOnlyReason {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}

type migrationWarningCollectorKey struct{}

type configSnapshotKey struct{}

type configSnapshot struct {
	path     string
	contents []byte
}

// withExplicitLegacyMigrationConsent mints path-bound consent only for the
// high-level explicit loader used by --config and GCX_CONFIG. The generic
// ExplicitConfigFile Source remains a path resolver and cannot authorize reads
// from predictable legacy keychain accounts.
func withExplicitLegacyMigrationConsent(ctx context.Context, path string) (context.Context, error) {
	identity, err := canonicalConfigSource(path)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, explicitLegacyMigrationConsentKey{}, explicitLegacyMigrationConsent{sourceIdentity: identity}), nil
}

func withConfigLayer(ctx context.Context, layer string) context.Context {
	return context.WithValue(ctx, configLayerKey{}, layer)
}

// ContextWithConfigSource preserves auto-discovery provenance across raw
// mutation helpers that pass a Source separately. In particular, local
// repository sources remain no-symlink even when a provider reloads them by
// explicit path before writing.
func ContextWithConfigSource(ctx context.Context, source ConfigSource) context.Context {
	return withConfigLayer(ctx, source.Type)
}

func configLayerFromCtx(ctx context.Context) string {
	layer, _ := ctx.Value(configLayerKey{}).(string)
	return layer
}

func withMigrationPersistenceSuppressed(ctx context.Context) context.Context {
	return context.WithValue(ctx, migrationPersistenceKey{}, true)
}

func migrationPersistenceSuppressed(ctx context.Context) bool {
	suppressed, _ := ctx.Value(migrationPersistenceKey{}).(bool)
	return suppressed
}

func withInMemoryMigrationWarningCollector(ctx context.Context, collector *inMemoryMigrationWarningCollector) context.Context {
	return context.WithValue(ctx, migrationWarningCollectorKey{}, collector)
}

func inMemoryMigrationWarningCollectorFromContext(ctx context.Context) *inMemoryMigrationWarningCollector {
	collector, _ := ctx.Value(migrationWarningCollectorKey{}).(*inMemoryMigrationWarningCollector)
	return collector
}

func withConfigSnapshot(ctx context.Context, path string, contents []byte) context.Context {
	return context.WithValue(ctx, configSnapshotKey{}, configSnapshot{
		path: path, contents: bytes.Clone(contents),
	})
}

func configSnapshotFromContext(ctx context.Context, path string) ([]byte, bool) {
	snapshot, ok := ctx.Value(configSnapshotKey{}).(configSnapshot)
	if !ok || snapshot.path != path {
		return nil, false
	}
	return bytes.Clone(snapshot.contents), true
}

// writeLegacyBackup writes an exact byte-for-byte copy next to the logical
// config path, once: an existing backup is never overwritten (an old gcx binary
// running concurrently can rewrite the file to the legacy format and trigger
// re-migration, which must not clobber a known-good backup). A false result
// includes an actionable reason; the caller must not replace the legacy file
// without rollback safety.
func writeLegacyBackup(filename string, contents []byte, codec *format.YAMLCodec) (bool, error) {
	backupPath := filename + legacyBackupSuffix

	backup, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, configFilePermissions)
	if errors.Is(err, os.ErrExist) {
		if err := validateExistingLegacyBackup(backupPath, contents, codec); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not create legacy config backup %s: %w", backupPath, err)
	}
	complete := false
	defer func() {
		_ = backup.Close()
		if !complete {
			_ = os.Remove(backupPath)
		}
	}()
	if _, err := backup.Write(contents); err != nil {
		return false, fmt.Errorf("could not write legacy config backup %s: %w", backupPath, err)
	}
	if err := backup.Sync(); err != nil {
		return false, fmt.Errorf("could not sync legacy config backup %s: %w", backupPath, err)
	}
	if err := backup.Close(); err != nil {
		return false, fmt.Errorf("could not close legacy config backup %s: %w", backupPath, err)
	}
	if err := syncConfigDirectory(filepath.Dir(backupPath)); err != nil {
		return false, fmt.Errorf("could not sync legacy config backup directory for %s: %w", backupPath, err)
	}
	complete = true
	return true, nil
}

// validateExistingLegacyBackup refuses to treat an arbitrary, partial, or
// symlinked path as rollback safety. A prior complete legacy backup is valid;
// anything else leaves migration in memory and the original source untouched.
func validateExistingLegacyBackup(path string, expected []byte, codec *format.YAMLCodec) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("could not inspect existing legacy config backup %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("existing legacy config backup is not a regular file: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("existing legacy config backup has insecure permissions: %s (mode %s)", path, info.Mode().Perm())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("could not read existing legacy config backup %s: %w", path, err)
	}
	if !bytes.Equal(contents, expected) {
		return fmt.Errorf("existing legacy config backup does not match the current source; refusing to replace it: %s", path)
	}
	if version, present, _ := declaredConfigVersion(contents); present {
		return fmt.Errorf("existing legacy config backup is versioned and cannot be used for rollback: %s (version %d)", path, version)
	}
	var shape map[string]any
	if err := yaml.Unmarshal(contents, &shape); err != nil {
		return fmt.Errorf("existing legacy config backup %s is invalid: %w", path, err)
	}
	if _, ok := shape["contexts"]; !ok {
		return fmt.Errorf("existing legacy config backup has no contexts block: %s", path)
	}
	if _, current := shape["stacks"]; current {
		return fmt.Errorf("existing legacy config backup uses the current schema: %s", path)
	}
	if _, current := shape["cloud"]; current {
		return fmt.Errorf("existing legacy config backup uses the current schema: %s", path)
	}
	var legacy legacyConfig
	if err := codec.Decode(bytes.NewReader(contents), &legacy); err != nil {
		return fmt.Errorf("existing legacy config backup %s is invalid: %w", path, err)
	}
	return nil
}
