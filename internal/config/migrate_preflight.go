package config

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"

	"github.com/goccy/go-yaml"
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
// proven interrupted migration may finish a remaining legacy layer while an
// unproven overlap remains fail-closed with edit-only recovery instructions.
type layeredMigrationIncompleteError struct {
	cause                    error
	remaining                []ConfigSource
	targetedMigrationAllowed bool
}

func (e *layeredMigrationIncompleteError) Error() string {
	var b strings.Builder
	b.WriteString("layered configuration migration is incomplete")
	if e.cause != nil {
		fmt.Fprintf(&b, " (%v)", e.cause)
	}
	if e.targetedMigrationAllowed {
		b.WriteString("; no additional config files or credentials were changed. Migrate each remaining file:\n")
		b.WriteString(layeredMigrationSteps(e.remaining))
		b.WriteByte('\n')
		b.WriteString(layeredMigrationGuidance(e.remaining))
		return b.String()
	}
	b.WriteString("; no config files or credentials were changed. The overlapping entries require manual consolidation; edit the remaining legacy layers directly, then retry:\n")
	b.WriteString(layeredMigrationRepairSteps(e.remaining))
	b.WriteString("\n  Guide: " + docs.ConfigMigration)
	return b.String()
}

func (e *layeredMigrationIncompleteError) Unwrap() error { return e.cause }

func (e *layeredMigrationIncompleteError) includesLayer(layerType string) bool {
	if !e.targetedMigrationAllowed {
		return false
	}
	for _, source := range e.remaining {
		if source.Type == layerType {
			return true
		}
	}
	return false
}

func layeredMigrationRepairSteps(sources []ConfigSource) string {
	var b strings.Builder
	for i, source := range sources {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  %s\n", source.Path)
		fmt.Fprintf(&b, "    repair: gcx config edit %s", source.Type)
	}
	return b.String()
}

func layeredMigrationSteps(sources []ConfigSource) string {
	var b strings.Builder
	for i, source := range sources {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  gcx config set --file %s version 1    # migrates %s", source.Type, source.Path)
	}
	return b.String()
}

// layeredMigrationGuidance explains what the migration commands do and how to
// recover when one fails, naming the per-layer edit command for each file.
func layeredMigrationGuidance(sources []ConfigSource) string {
	return "  Each command rewrites that file in the current format and keeps a backup next to it (" + legacyBackupSuffix + ").\n" +
		"  If a command reports a problem, fix that file by hand (" + layeredMigrationEditCommands(sources) + ") and re-run it.\n" +
		"  Guide: " + docs.ConfigMigration
}

// layeredMigrationEditCommands lists the per-layer edit commands, for recovery
// hints in warnings and structured logs.
func layeredMigrationEditCommands(sources []ConfigSource) string {
	edits := make([]string, 0, len(sources))
	for _, source := range sources {
		edits = append(edits, "gcx config edit "+source.Type)
	}
	return strings.Join(edits, ", ")
}

func writeExceptionalMigrationWarnings(writer io.Writer, warnings []inMemoryMigrationWarning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprint(writer, "\n  blockers discovered while loading the legacy sources:")
	for _, warning := range warnings {
		fmt.Fprintf(writer, "\n  %s\n    reason: %s", warning.filename, warning.reason)
	}
}

func formatExceptionalMigrationWarnings(warnings []inMemoryMigrationWarning) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	writeExceptionalMigrationWarnings(&b, warnings)
	return strings.TrimPrefix(b.String(), "\n")
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
			// A mixed state is recoverable with targeted per-layer migration only
			// when every current layer is provably the output of this migrator and
			// the original all-legacy set passed the same semantic-equivalence proof
			// used before the first file changed. An arbitrary v1 file plus a partial
			// legacy overlay must never receive a command that atomically shadows the
			// complete entry below it.
			reconstructed, reconstructErr := reconstructInterruptedLegacyLayers(layers)
			allowTargeted := reconstructErr == nil && proveLayeredLegacyConversion(reconstructed) == nil
			return &layeredMigrationIncompleteError{
				cause:                    err,
				remaining:                remainingLegacySources(layers),
				targetedMigrationAllowed: allowTargeted,
			}
		}
	}
	if !hasLegacy || !allLegacy || len(layers) < 2 {
		return nil
	}

	if err := proveLayeredLegacyConversion(layers); err != nil {
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

// reconstructInterruptedLegacyLayers proves that a mixed configuration is an
// interrupted migration rather than an arbitrary combination of current and
// legacy files. Every current layer must have a secure legacy backup whose
// conversion matches the current document apart from opaque credential values.
func reconstructInterruptedLegacyLayers(layers []migrationLayer) ([]migrationLayer, error) {
	reconstructed := make([]migrationLayer, 0, len(layers))
	for _, layer := range layers {
		if layer.legacy != nil {
			reconstructed = append(reconstructed, layer)
			continue
		}
		backupPath := layer.source.Path + legacyBackupSuffix
		info, err := os.Lstat(backupPath)
		if err != nil {
			return nil, fmt.Errorf("inspect migration backup %s: %w", backupPath, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("migration backup %s is not a private regular file", backupPath)
		}
		contents, err := os.ReadFile(backupPath)
		if err != nil {
			return nil, fmt.Errorf("read migration backup %s: %w", backupPath, err)
		}
		if err := validateDeclaredConfigVersion(backupPath, contents); err != nil || !isLegacyConfig(contents) {
			return nil, fmt.Errorf("migration backup %s is not a valid legacy config", backupPath)
		}
		var legacy legacyConfig
		codec := &format.YAMLCodec{BytesAsBase64: true}
		if err := codec.Decode(bytes.NewReader(contents), &legacy); err != nil {
			return nil, UnmarshalError{File: backupPath, Err: err}
		}
		converted := convertLegacyConfig(&legacy, layer.source.Type, nil)
		matches, err := sameMigrationDocumentShape(converted, layer.current)
		if err != nil {
			return nil, err
		}
		if !matches {
			return nil, fmt.Errorf("current config %s does not match its migration backup", layer.source.Path)
		}
		reconstructed = append(reconstructed, migrationLayer{
			source:   layer.source,
			contents: contents,
			legacy:   &legacy,
		})
	}
	return reconstructed, nil
}

func sameMigrationDocumentShape(expected, actual *Config) (bool, error) {
	want, err := migrationDocumentShape(expected)
	if err != nil {
		return false, err
	}
	got, err := migrationDocumentShape(actual)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(want, got), nil
}

func migrationDocumentShape(cfg *Config) (map[string]any, error) {
	codec := &format.YAMLCodec{BytesAsBase64: true}
	var encoded bytes.Buffer
	if err := codec.Encode(&encoded, cfg); err != nil {
		return nil, err
	}
	var clone Config
	if err := codec.Decode(bytes.NewReader(encoded.Bytes()), &clone); err != nil {
		return nil, err
	}
	for _, stack := range clone.Stacks {
		if stack == nil {
			continue
		}
		if stack.Grafana != nil {
			stack.Grafana.APIToken = configuredCredentialMarker(stack.Grafana.APIToken)
			stack.Grafana.Password = configuredCredentialMarker(stack.Grafana.Password)
			stack.Grafana.OAuthToken = configuredCredentialMarker(stack.Grafana.OAuthToken)
			stack.Grafana.OAuthRefreshToken = configuredCredentialMarker(stack.Grafana.OAuthRefreshToken)
		}
		if synth := stack.Providers["synth"]; synth != nil {
			if value, ok := synth["sm-token"]; ok {
				synth["sm-token"] = configuredCredentialMarker(value)
			}
		}
	}
	for _, entry := range clone.Cloud {
		if entry == nil {
			continue
		}
		entry.Token = configuredCredentialMarker(entry.Token)
		entry.OAuthToken = configuredCredentialMarker(entry.OAuthToken)
	}
	encoded.Reset()
	if err := codec.Encode(&encoded, &clone); err != nil {
		return nil, err
	}
	var shape map[string]any
	if err := yaml.Unmarshal(encoded.Bytes(), &shape); err != nil {
		return nil, err
	}
	return shape, nil
}

func configuredCredentialMarker(value string) string {
	if value == "" {
		return ""
	}
	return "<configured>"
}

// proveLayeredLegacyConversion compares the accepted pre-v1 field-level merge
// with independent conversion followed by v1 atomic-entry merge.
func proveLayeredLegacyConversion(layers []migrationLayer) error {
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
	return compareMigratedEffectiveViews(expected, &actual)
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
						"cannot safely auto-migrate mixed layered configuration: stack entry %q overlaps between %s and %s; no config files or credentials were changed; recovery must be verified before any layer changes (%s)",
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
						"cannot safely auto-migrate mixed layered configuration: cloud entry %q overlaps between %s and %s; no config files or credentials were changed; recovery must be verified before any layer changes (%s)",
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
