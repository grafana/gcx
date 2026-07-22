package config

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
)

type migrationLayer struct {
	source   ConfigSource
	contents []byte
	legacy   *legacyConfig
	current  *Config
}

// layeredMigrationIncompleteError describes an interrupted (or manually
// created) mixed-schema layered configuration. It is deliberately typed so a
// targeted --file write may finish one of the remaining legacy layers while
// ordinary loads still fail with deterministic recovery instructions.
type layeredMigrationIncompleteError struct {
	cause     error
	remaining []ConfigSource
}

func (e *layeredMigrationIncompleteError) Error() string {
	var b strings.Builder
	b.WriteString("layered configuration migration is incomplete")
	if e.cause != nil {
		fmt.Fprintf(&b, " (%v)", e.cause)
	}
	b.WriteString("; no additional config files or credentials were changed. Complete every remaining legacy layer:\n")
	b.WriteString(layeredMigrationSteps(e.remaining))
	return b.String()
}

func (e *layeredMigrationIncompleteError) Unwrap() error { return e.cause }

func (e *layeredMigrationIncompleteError) includesLayer(layerType string) bool {
	for _, source := range e.remaining {
		if source.Type == layerType {
			return true
		}
	}
	return false
}

func layeredMigrationSteps(sources []ConfigSource) string {
	var b strings.Builder
	for i, source := range sources {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  %s\n", source.Path)
		fmt.Fprintf(&b, "    migrate: gcx config set --file %s version 1\n", source.Type)
		fmt.Fprintf(&b, "    repair:  gcx config edit %s", source.Type)
	}
	return b.String()
}

func remainingLegacySources(layers []migrationLayer) []ConfigSource {
	remaining := make([]ConfigSource, 0, len(layers))
	for _, layer := range layers {
		if layer.legacy != nil {
			remaining = append(remaining, layer.source)
		}
	}
	return remaining
}

// preflightLayeredSources validates every discovered source before Load is
// allowed to migrate or touch credentials. When all contributing files use the
// legacy schema, it also proves that independent per-file conversion followed
// by v1's atomic entry merge preserves the effective legacy view. A partial
// overlay that cannot cross that boundary safely is left entirely untouched for
// manual migration.
func preflightLayeredSources(sources []ConfigSource, legacyFound ...*bool) error {
	layers := make([]migrationLayer, 0, len(sources))
	hasLegacy := false
	allLegacy := len(sources) > 0

	for i := range sources {
		source := sources[i]
		contents, err := readConfigSource(source)
		if err != nil {
			return err
		}
		sources[i].snapshot = bytes.Clone(contents)
		if err := validateDeclaredConfigVersion(source.Path, contents); err != nil {
			return err
		}

		layer := migrationLayer{source: source, contents: contents}
		if isLegacyConfig(contents) {
			decoded := &legacyConfig{}
			codec := &format.YAMLCodec{BytesAsBase64: true}
			if err := codec.Decode(bytes.NewReader(contents), decoded); err != nil {
				return UnmarshalError{File: source.Path, Err: err}
			}
			layer.legacy = decoded
			if err := validateLegacyLayerReferences(source, decoded); err != nil {
				return err
			}
			hasLegacy = true
		} else {
			decoded := &Config{}
			codec := &format.YAMLCodec{BytesAsBase64: true}
			if err := codec.Decode(bytes.NewReader(contents), decoded); err != nil {
				return UnmarshalError{File: source.Path, Err: err}
			}
			layer.current = decoded
			allLegacy = false
		}
		layers = append(layers, layer)
	}
	if len(legacyFound) > 0 && legacyFound[0] != nil {
		*legacyFound[0] = hasLegacy
	}

	if hasLegacy && !allLegacy {
		if err := rejectMixedLayerEntryOverlap(layers); err != nil {
			return &layeredMigrationIncompleteError{
				cause:     err,
				remaining: remainingLegacySources(layers),
			}
		}
	}
	if !hasLegacy || !allLegacy || len(layers) < 2 {
		return nil
	}

	// Decode fresh copies for the old-semantics merge. The merge helpers are
	// intentionally small and may reuse/mutate nested maps; using the layer
	// objects themselves would contaminate the independent-conversion side of
	// the comparison and weaken the proof.
	codec := &format.YAMLCodec{BytesAsBase64: true}
	var legacyMerged legacyConfig
	if err := codec.Decode(bytes.NewReader(layers[0].contents), &legacyMerged); err != nil {
		return UnmarshalError{File: layers[0].source.Path, Err: err}
	}
	for _, layer := range layers[1:] {
		var overlay legacyConfig
		if err := codec.Decode(bytes.NewReader(layer.contents), &overlay); err != nil {
			return UnmarshalError{File: layer.source.Path, Err: err}
		}
		legacyMerged = mergeLegacyConfigs(legacyMerged, overlay)
	}
	expected := convertLegacyConfig(&legacyMerged, "", nil)

	var actual Config
	for i, layer := range layers {
		converted := convertLegacyConfig(layer.legacy, layer.source.Type, nil)
		if i == 0 {
			actual = *converted
		} else {
			actual = MergeConfigs(actual, *converted)
		}
	}

	if err := compareMigratedEffectiveViews(expected, &actual); err != nil {
		paths := make([]string, 0, len(sources))
		for _, source := range sources {
			paths = append(paths, source.Path)
		}
		return fmt.Errorf(
			"cannot safely auto-migrate layered legacy configuration (%w); no config files or credentials were changed; consolidate the overlapping context fields or migrate the files manually (%s): %s",
			err, docs.ConfigMigration, strings.Join(paths, ", "))
	}
	return nil
}

func validateLegacyLayerReferences(source ConfigSource, legacy *legacyConfig) error {
	trusted := source.Type == "user" && trustedDiscoveredUserLegacySource(source.Path)
	for name, legacyContext := range legacy.Contexts {
		if legacyContext == nil {
			continue
		}
		for _, field := range credentials.AllFields {
			ref, ok := legacySecretRef(legacyContext, field)
			if !ok || !credentials.IsSentinel(ref.get()) {
				continue
			}
			if !trusted {
				return fmt.Errorf(
					"legacy keychain reference for context %q field %q cannot be auto-migrated from %s config %s; no config files or credentials were changed; migrate that layer explicitly after replacing the reference (%s)",
					name, field, source.Type, source.Path, docs.ConfigMigration,
				)
			}
			owner, sentinelField, valid := credentials.ParseSentinel(ref.get())
			if !valid || owner != name || sentinelField != field {
				return fmt.Errorf(
					"invalid legacy keychain reference for context %q field %q in %s; no config files or credentials were changed (%s)",
					name, field, source.Path, docs.ConfigMigration,
				)
			}
		}
	}
	return nil
}

func rejectMixedLayerEntryOverlap(layers []migrationLayer) error {
	type ownerSource struct {
		kind   string
		path   string
		legacy bool
	}
	seen := map[string]ownerSource{}
	for _, layer := range layers {
		var candidate *Config
		if layer.legacy != nil {
			candidate = convertLegacyConfig(layer.legacy, layer.source.Type, nil)
		} else {
			candidate = layer.current
		}
		for name := range candidate.Stacks {
			key := "stack\x00" + name
			if prior, exists := seen[key]; exists {
				if prior.legacy || layer.legacy != nil {
					return fmt.Errorf(
						"cannot safely auto-migrate mixed layered configuration: stack entry %q overlaps between %s and %s; no config files or credentials were changed; migrate layers explicitly (%s)",
						name, prior.path, layer.source.Path, docs.ConfigMigration,
					)
				}
			}
			seen[key] = ownerSource{kind: "stack", path: layer.source.Path, legacy: layer.legacy != nil}
		}
		for name := range candidate.Cloud {
			key := "cloud\x00" + name
			if prior, exists := seen[key]; exists {
				if prior.legacy || layer.legacy != nil {
					return fmt.Errorf(
						"cannot safely auto-migrate mixed layered configuration: cloud entry %q overlaps between %s and %s; no config files or credentials were changed; migrate layers explicitly (%s)",
						name, prior.path, layer.source.Path, docs.ConfigMigration,
					)
				}
			}
			seen[key] = ownerSource{kind: "cloud", path: layer.source.Path, legacy: layer.legacy != nil}
		}
	}
	return nil
}

// mergeLegacyConfigs reproduces the accepted pre-v1 field-level layering
// semantics. It exists only for the no-side-effect migration preflight.
func mergeLegacyConfigs(base, over legacyConfig) legacyConfig {
	result := base
	if over.CurrentContext != "" {
		result.CurrentContext = over.CurrentContext
	}
	if over.Contexts != nil {
		if result.Contexts == nil {
			result.Contexts = map[string]*legacyContext{}
		}
		for name, overCtx := range over.Contexts {
			if baseCtx, ok := result.Contexts[name]; ok {
				result.Contexts[name] = mergeLegacyContexts(baseCtx, overCtx)
			} else {
				result.Contexts[name] = overCtx
			}
		}
	}
	if over.Diagnostics != nil {
		if result.Diagnostics == nil {
			result.Diagnostics = over.Diagnostics
		} else {
			merged := mergeDiagnosticsConfig(result.Diagnostics, over.Diagnostics)
			result.Diagnostics = &merged
		}
	}
	return result
}

func mergeLegacyContexts(base, over *legacyContext) *legacyContext {
	if base == nil {
		return over
	}
	if over == nil {
		return base
	}
	result := *base

	if over.Grafana != nil {
		if result.Grafana == nil {
			result.Grafana = over.Grafana
		} else {
			merged := mergeLegacyGrafana(result.Grafana, over.Grafana)
			result.Grafana = &merged
		}
	}
	if over.Cloud != nil {
		if result.Cloud == nil {
			result.Cloud = over.Cloud
		} else {
			merged := mergeLegacyCloud(result.Cloud, over.Cloud)
			result.Cloud = &merged
		}
	}
	if over.Providers != nil {
		if result.Providers == nil {
			result.Providers = map[string]map[string]string{}
		}
		for provider, values := range over.Providers {
			merged := maps.Clone(result.Providers[provider])
			if merged == nil {
				merged = map[string]string{}
			}
			maps.Copy(merged, values)
			result.Providers[provider] = merged
		}
	}
	if over.Datasources != nil {
		if result.Datasources == nil {
			result.Datasources = map[string]string{}
		}
		maps.Copy(result.Datasources, over.Datasources)
	}
	if over.Resources != nil {
		if result.Resources == nil {
			result.Resources = over.Resources
		} else {
			merged := mergeResourcesConfig(result.Resources, over.Resources)
			result.Resources = &merged
		}
	}
	if over.DefaultPrometheusDatasource != "" {
		result.DefaultPrometheusDatasource = over.DefaultPrometheusDatasource
	}
	if over.DefaultLokiDatasource != "" {
		result.DefaultLokiDatasource = over.DefaultLokiDatasource
	}
	if over.DefaultPyroscopeDatasource != "" {
		result.DefaultPyroscopeDatasource = over.DefaultPyroscopeDatasource
	}
	// Deliberately do not merge DefaultTempoDatasource here. The accepted
	// pre-v1 MergeConfigs implementation omitted that field, and migration
	// preflight must reproduce the old effective semantics exactly—even where
	// those semantics contained a bug—before deciding whether conversion is
	// lossless.
	return &result
}

func mergeLegacyGrafana(base, over *GrafanaConfig) GrafanaConfig {
	result := *base
	if over.Server != "" {
		result.Server = over.Server
	}
	if over.User != "" {
		result.User = over.User
	}
	if over.Password != "" {
		result.Password = over.Password
	}
	if over.APIToken != "" {
		result.APIToken = over.APIToken
	}
	if over.OrgID != 0 {
		result.OrgID = over.OrgID
	}
	if over.StackID != 0 {
		result.StackID = over.StackID
	}
	if over.TLS != nil {
		tlsCopy := *over.TLS
		result.TLS = &tlsCopy
	}
	if over.OAuthToken != "" {
		result.OAuthToken = over.OAuthToken
	}
	if over.OAuthRefreshToken != "" {
		result.OAuthRefreshToken = over.OAuthRefreshToken
	}
	if over.OAuthTokenExpiresAt != "" {
		result.OAuthTokenExpiresAt = over.OAuthTokenExpiresAt
	}
	if over.OAuthRefreshExpiresAt != "" {
		result.OAuthRefreshExpiresAt = over.OAuthRefreshExpiresAt
	}
	if over.ProxyEndpoint != "" {
		result.ProxyEndpoint = over.ProxyEndpoint
	}
	return result
}

func mergeLegacyCloud(base, over *legacyCloudConfig) legacyCloudConfig {
	result := *base
	if over.Token != "" {
		result.Token = over.Token
	}
	if over.Stack != "" {
		result.Stack = over.Stack
	}
	if over.OAuthUrl != "" {
		result.OAuthUrl = over.OAuthUrl
	}
	if over.APIUrl != "" {
		result.APIUrl = over.APIUrl
	}
	return result
}

func compareMigratedEffectiveViews(expected, actual *Config) error {
	expected.Resolve()
	actual.Resolve()
	if expected.CurrentContext != actual.CurrentContext {
		return fmt.Errorf("current-context changed from %q to %q", expected.CurrentContext, actual.CurrentContext)
	}
	if len(expected.Contexts) != len(actual.Contexts) {
		return fmt.Errorf("context count changed from %d to %d", len(expected.Contexts), len(actual.Contexts))
	}
	for name, want := range expected.Contexts {
		got := actual.Contexts[name]
		if got == nil {
			return fmt.Errorf("context %q disappeared", name)
		}
		if !reflect.DeepEqual(want.Grafana, got.Grafana) {
			return fmt.Errorf("context %q grafana connection/auth changed", name)
		}
		if !reflect.DeepEqual(want.Providers, got.Providers) {
			return fmt.Errorf("context %q provider configuration changed", name)
		}
		if !reflect.DeepEqual(want.Datasources, got.Datasources) {
			return fmt.Errorf("context %q datasource defaults changed", name)
		}
		if !reflect.DeepEqual(want.AssumeServerDryRun(), got.AssumeServerDryRun()) {
			return fmt.Errorf("context %q resource settings changed", name)
		}
		if want.ResolveStackSlug() != got.ResolveStackSlug() {
			return fmt.Errorf("context %q stack slug changed", name)
		}
		if !reflect.DeepEqual(publicCloudView(want.CloudEntry), publicCloudView(got.CloudEntry)) {
			return fmt.Errorf("context %q cloud credential or endpoint changed", name)
		}
	}
	return nil
}

func publicCloudView(entry *CloudEntry) any {
	if entry == nil {
		return nil
	}
	return struct {
		Token               string
		OAuthToken          string
		OAuthTokenExpiresAt string
		OAuthScopes         []string
		OAuthURL            string
		APIURL              string
	}{entry.Token, entry.OAuthToken, entry.OAuthTokenExpiresAt, entry.OAuthScopes, entry.OAuthUrl, entry.APIUrl}
}
