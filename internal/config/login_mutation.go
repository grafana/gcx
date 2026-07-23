package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
)

// LoginMutationIntent describes which atomic entries a login operation may
// update. Unified login always refreshes Grafana auth and may also add or
// refresh Cloud auth. Cloud login only updates the context's Cloud entry.
//
// This intent is deliberately separate from generic config mutation routing:
// layered login may infer an existing entry's owner, while an arbitrary config
// mutation must continue to require an explicit target when multiple sources
// are present.
type LoginMutationIntent string

const (
	LoginMutationUnified LoginMutationIntent = "unified"
	LoginMutationCloud   LoginMutationIntent = "cloud"
)

// LoginMutationGuard pins the exact raw owner snapshot loaded before an
// authentication flow starts. Its fields are private so callers cannot forge
// revision evidence; the zero value deliberately disables the check for
// existing programmatic callers that do not perform a pre-auth load.
type LoginMutationGuard struct {
	enabled              bool
	sourceIdentity       string
	sourcePath           string
	sourceLayer          string
	sourceRevision       [32]byte
	hasSourceRevision    bool
	expectSourceAbsent   bool
	contextName          string
	intent               LoginMutationIntent
	expectContextPresent bool
	expectStack          string
	expectCloud          string
	verifyDiscovery      bool
	discoveredSources    []loginMutationSourceSnapshot
}

type loginMutationSourceSnapshot struct {
	path     string
	typeName string
	identity string
	revision [32]byte
}

// NewLoginMutationGuard captures the selected raw config's revision and the
// intent-relevant context bindings before authentication begins.
func (config *Config) NewLoginMutationGuard(contextName string, intent LoginMutationIntent) LoginMutationGuard {
	guard := LoginMutationGuard{
		enabled:            true,
		sourceIdentity:     config.sourceIdentity,
		sourcePath:         config.Source,
		sourceLayer:        config.sourceLayer,
		sourceRevision:     config.sourceRevision,
		hasSourceRevision:  config.hasSourceRevision,
		expectSourceAbsent: config.expectSourceAbsent,
		contextName:        contextName,
		intent:             intent,
	}
	if ctx := config.Contexts[contextName]; ctx != nil {
		guard.expectContextPresent = true
		guard.expectStack = ctx.Stack
		guard.expectCloud = ctx.Cloud
	}
	return guard
}

// LoadLoginMutationGuarded decodes the selected owner from the exact bytes
// approved after authentication without allowing Load's automatic plaintext
// or legacy migration to write first. The guard is checked before the load,
// against the loaded snapshot, and once more against disk; the eventual Write
// CAS then protects the selected owner after this function returns.
func LoadLoginMutationGuarded(ctx context.Context, source Source, guard LoginMutationGuard) (Config, error) {
	if !guard.enabled {
		return Load(ctx, source)
	}

	snapshot, err := guard.currentSelectedSourceSnapshot()
	if err != nil {
		return Config{}, err
	}
	if guard.verifyDiscovery {
		matches, reason := guard.discoverySnapshotMatches()
		if !matches {
			return Config{}, guard.changedDuringAuthenticationError(reason)
		}
	}

	loadCtx := withMigrationPersistenceSuppressed(ctx)
	if snapshot != nil {
		loadCtx = withConfigSnapshot(loadCtx, guard.sourcePath, snapshot)
	}
	cfg, loadErr := Load(loadCtx, source)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return cfg, loadErr
	}
	if err := guard.VerifyCurrentSources(); err != nil {
		return Config{}, err
	}
	if err := guard.Verify(&cfg); err != nil {
		return Config{}, err
	}
	if snapshot != nil && !isLegacyConfig(snapshot) {
		// Migration persistence suppression is used here solely to make Load
		// side-effect-free. A verified v1 snapshot may proceed to the one
		// intentional final Write; a legacy snapshot remains deferred.
		cfg.migrationDeferred = false
	}
	return cfg, loadErr
}

// VerifyCurrentSources rechecks both the selected raw owner and, for
// auto-discovered login, every contributing source. Call it immediately before
// the intentional Write so a non-target layer cannot change after decoding.
func (guard LoginMutationGuard) VerifyCurrentSources() error {
	if !guard.enabled {
		return nil
	}
	if _, err := guard.currentSelectedSourceSnapshot(); err != nil {
		return err
	}
	if guard.verifyDiscovery {
		matches, reason := guard.discoverySnapshotMatches()
		if !matches {
			return guard.changedDuringAuthenticationError(reason)
		}
	}
	return nil
}

func (guard LoginMutationGuard) currentSelectedSourceSnapshot() ([]byte, error) {
	contents, err := readConfigSource(ConfigSource{Path: guard.sourcePath, Type: guard.sourceLayer})
	if guard.expectSourceAbsent {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, guard.changedDuringAuthenticationError("the selected config appeared after authentication started")
	}
	if err != nil {
		return nil, guard.changedDuringAuthenticationError(fmt.Sprintf("read selected config: %v", err))
	}
	identity, err := canonicalConfigSourceForLayer(guard.sourcePath, guard.sourceLayer)
	if err != nil {
		return nil, guard.changedDuringAuthenticationError(fmt.Sprintf("identify selected config: %v", err))
	}
	if identity != guard.sourceIdentity || !guard.hasSourceRevision || sha256.Sum256(contents) != guard.sourceRevision {
		return nil, guard.changedDuringAuthenticationError("the selected config changed")
	}
	return contents, nil
}

// WithDiscoverySnapshot extends a raw-owner guard to every source that
// contributed to an auto-discovered config. This matters even when discovery
// initially found only one file: a repository config can appear while OAuth is
// in progress and otherwise shadow the context or entry receiving the fresh
// credential. Explicit --config/GCX_CONFIG callers deliberately do not enable
// this check; their selected document is authoritative.
func (guard LoginMutationGuard) WithDiscoverySnapshot(effective *Config) (LoginMutationGuard, error) {
	if effective == nil {
		return guard, errors.New("capture login discovery snapshot: nil effective config")
	}
	guard.verifyDiscovery = true
	guard.discoveredSources = make([]loginMutationSourceSnapshot, 0, len(effective.Sources))
	for _, source := range effective.Sources {
		contents := source.snapshot
		if contents == nil {
			var err error
			contents, err = readConfigSource(source)
			if err != nil {
				return guard, fmt.Errorf("capture login source %s: %w", source.Path, err)
			}
		}
		identity, err := canonicalConfigSourceForLayer(source.Path, source.Type)
		if err != nil {
			return guard, fmt.Errorf("identify login source %s: %w", source.Path, err)
		}
		guard.discoveredSources = append(guard.discoveredSources, loginMutationSourceSnapshot{
			path:     source.Path,
			typeName: source.Type,
			identity: identity,
			revision: sha256.Sum256(contents),
		})
	}

	matches, reason := guard.discoverySnapshotMatches()
	if !matches {
		return guard, guard.changedDuringPlanningError(reason)
	}
	return guard, nil
}

// Verify rejects a post-auth reload unless it is the exact raw snapshot that
// was approved before authentication. This extends the ordinary Write CAS
// across the authentication interval: a later Load must not reset the CAS
// baseline after a context, destination, or credential owner changes.
func (guard LoginMutationGuard) Verify(config *Config) error {
	if !guard.enabled {
		return nil
	}
	if config == nil {
		return guard.changedDuringAuthenticationError("the selected config could not be reloaded")
	}
	var revisionMatches bool
	switch {
	case guard.expectSourceAbsent:
		revisionMatches = config.expectSourceAbsent && !config.hasSourceRevision
	case guard.hasSourceRevision:
		revisionMatches = config.hasSourceRevision && config.sourceRevision == guard.sourceRevision
	default:
		revisionMatches = !config.hasSourceRevision
	}
	if config.sourceIdentity != guard.sourceIdentity || !revisionMatches {
		return guard.changedDuringAuthenticationError("the selected config changed")
	}
	if guard.verifyDiscovery {
		matches, reason := guard.discoverySnapshotMatches()
		if !matches {
			return guard.changedDuringAuthenticationError(reason)
		}
	}

	actual := config.Contexts[guard.contextName]
	if guard.expectContextPresent != (actual != nil) {
		return guard.changedDuringAuthenticationError("the selected context appeared or disappeared")
	}
	if actual == nil {
		return nil
	}
	if guard.intent == LoginMutationCloud && actual.Cloud == guard.expectCloud {
		return nil
	}
	if guard.intent == LoginMutationUnified &&
		actual.Stack == guard.expectStack && actual.Cloud == guard.expectCloud {
		return nil
	}
	return guard.changedDuringAuthenticationError("the selected context bindings changed")
}

func (guard LoginMutationGuard) discoverySnapshotMatches() (bool, string) {
	sources, err := DiscoverSources()
	if err != nil {
		return false, fmt.Sprintf("config source discovery failed: %v", err)
	}
	if len(sources) != len(guard.discoveredSources) {
		return false, "the discovered config source set changed"
	}
	for i, expected := range guard.discoveredSources {
		actual := sources[i]
		identity, err := canonicalConfigSourceForLayer(actual.Path, actual.Type)
		if err != nil {
			return false, fmt.Sprintf("identify config source %s: %v", actual.Path, err)
		}
		if actual.Path != expected.path || actual.Type != expected.typeName || identity != expected.identity {
			return false, "the discovered config source set changed"
		}
		contents, err := readConfigSource(actual)
		if err != nil {
			return false, fmt.Sprintf("read config source %s: %v", actual.Path, err)
		}
		if sha256.Sum256(contents) != expected.revision {
			return false, fmt.Sprintf("config source %s changed", actual.Path)
		}
	}
	return true, ""
}

func (guard LoginMutationGuard) changedDuringPlanningError(reason string) error {
	return gcxerrors.DetailedError{
		Summary: "Configuration changed during login planning",
		Details: fmt.Sprintf(
			"The discovered configuration changed before authentication started (%s). No authentication request was sent.",
			reason,
		),
		Suggestions: []string{
			"Review the changed config and retry login",
			"Or re-run with --config " + strconv.Quote(guard.sourcePath) + " after reviewing that owner",
		},
	}
}

func (guard LoginMutationGuard) changedDuringAuthenticationError(reason string) error {
	return gcxerrors.DetailedError{
		Summary: "Configuration changed during authentication",
		Details: fmt.Sprintf(
			"The configuration changed after authentication started (%s). The freshly authenticated credential was not written to %s.",
			reason,
			guard.sourcePath,
		),
		Suggestions: []string{
			"Review the changed config and retry login",
			"Or re-run with --config " + strconv.Quote(guard.sourcePath) + " after reviewing that owner",
		},
	}
}

// PlanLoginMutation selects the one existing source that can safely own a
// layered login mutation. Callers handle explicit selection and the
// zero/one-source cases before invoking this method.
//
// A source is eligible only when:
//   - the target context already exists;
//   - every atomic stack/Cloud entry the operation may update has that source;
//   - the raw source contains the context with every binding the operation may
//     mutate (stack+Cloud for unified login; Cloud for Cloud-only login).
//
// These checks ensure login reloads and mutates the same raw document that owns
// the credentials. In particular, it never copies a resolved keychain value
// from one layer into another layer. A singular local owner is returned with
// its "local" provenance intact; credential-accepting callers then allow only
// an unchanged bound credential and require explicit --config for fresh auth.
func (config *Config) PlanLoginMutation(contextName string, intent LoginMutationIntent) (ConfigSource, error) {
	if len(config.Sources) < 2 {
		return ConfigSource{}, errors.New("layered login planning requires at least two config sources")
	}
	if intent != LoginMutationUnified && intent != LoginMutationCloud {
		return ConfigSource{}, fmt.Errorf("unknown login mutation intent %q", intent)
	}

	if contextName == "" {
		contextName = ResolveContextName("", *config)
	}
	ctx := config.Contexts[contextName]
	if ctx == nil {
		return ConfigSource{}, config.loginMutationTargetError(
			contextName,
			fmt.Sprintf("Context %q does not exist in the effective layered configuration. Creating a context cannot be assigned to a layer implicitly.", contextName),
		)
	}

	cloudBindingIdentity, err := config.contextCloudBindingOwnerIdentity(contextName)
	if err != nil {
		return ConfigSource{}, config.loginMutationTargetError(contextName, err.Error())
	}
	ownerIdentities := loginMutationOwnerIdentities(ctx, intent, cloudBindingIdentity)
	if len(ownerIdentities) > 1 {
		ownerPaths := make([]string, 0, len(ownerIdentities))
		for _, identity := range ownerIdentities {
			if source, ok := config.sourceForIdentity(identity); ok {
				ownerPaths = append(ownerPaths, source.Path)
				continue
			}
			ownerPaths = append(ownerPaths, identity)
		}
		return ConfigSource{}, config.loginMutationTargetError(
			contextName,
			fmt.Sprintf(
				"Context %q resolves entries that this login may update from different files: %s. gcx will not copy resolved credentials between owners.",
				contextName,
				strings.Join(ownerPaths, ", "),
			),
		)
	}

	var candidates []ConfigSource
	if len(ownerIdentities) == 1 {
		if source, ok := config.sourceForIdentity(ownerIdentities[0]); ok {
			candidates = append(candidates, source)
		}
	} else {
		// A context with no resolved atomic entry still has a deterministic
		// owner when exactly one raw source contains its complete bindings.
		// This permits Cloud login to attach an entry to an existing context,
		// without inventing a user-layer shadow context.
		for _, source := range config.Sources {
			matches, err := sourceContainsContextBindings(source, contextName, ctx, intent)
			if err != nil {
				return ConfigSource{}, config.loginMutationTargetError(contextName, err.Error())
			}
			if matches {
				candidates = append(candidates, source)
			}
		}
	}

	if len(candidates) != 1 {
		detail := fmt.Sprintf("Context %q has no single existing config owner that can safely receive this login mutation.", contextName)
		if len(candidates) > 1 {
			paths := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				paths = append(paths, candidate.Path)
			}
			detail = fmt.Sprintf("Context %q has multiple indistinguishable config owners: %s.", contextName, strings.Join(paths, ", "))
		}
		return ConfigSource{}, config.loginMutationTargetError(contextName, detail)
	}

	target := candidates[0]
	matches, err := sourceContainsContextBindings(target, contextName, ctx, intent)
	if err != nil {
		return ConfigSource{}, config.loginMutationTargetError(contextName, err.Error())
	}
	if !matches {
		return ConfigSource{}, config.loginMutationTargetError(
			contextName,
			fmt.Sprintf(
				"The credential owner %s does not contain context %q with the effective stack/cloud bindings (%q / %q).",
				target.Path,
				contextName,
				ctx.Stack,
				ctx.Cloud,
			),
		)
	}
	if intent == LoginMutationUnified && ctx.Stack == "" && config.Stacks[contextName] != nil {
		targetIdentity, err := canonicalConfigSourceForLayer(target.Path, target.Type)
		if err != nil {
			return ConfigSource{}, config.loginMutationTargetError(contextName, err.Error())
		}
		if config.Stacks[contextName].sourceIdentity != targetIdentity {
			return ConfigSource{}, config.loginMutationTargetError(
				contextName,
				fmt.Sprintf(
					"Stack entry %q already exists outside the selected context owner %s. Creating the same name there would leave the fresh credential shadowed or overwrite unrelated stack settings.",
					contextName,
					target.Path,
				),
			)
		}
	}

	return target, nil
}

// loginMutationOwnerIdentities returns a stable, de-duplicated list. Cloud
// login prefers the Cloud entry owner and only falls back to the stack/context
// owner when no Cloud entry exists. Unified login may update both entries and
// therefore includes both owners.
func loginMutationOwnerIdentities(ctx *Context, intent LoginMutationIntent, cloudBindingIdentity string) []string {
	var identities []string
	appendIdentity := func(identity string) {
		if identity == "" || slices.Contains(identities, identity) {
			return
		}
		identities = append(identities, identity)
	}

	switch intent {
	case LoginMutationCloud:
		if ctx.CloudEntry != nil {
			appendIdentity(ctx.CloudEntry.sourceIdentity)
		}
		appendIdentity(cloudBindingIdentity)
		if len(identities) == 0 && ctx.StackEntry != nil {
			appendIdentity(ctx.StackEntry.sourceIdentity)
		}
	case LoginMutationUnified:
		if ctx.StackEntry != nil {
			appendIdentity(ctx.StackEntry.sourceIdentity)
		}
		if ctx.CloudEntry != nil {
			appendIdentity(ctx.CloudEntry.sourceIdentity)
		}
		appendIdentity(cloudBindingIdentity)
	}

	return identities
}

// contextCloudBindingOwnerIdentity returns the highest-priority source that
// explicitly supplies context.cloud. Contexts merge field-by-field, so entry
// ownership alone is insufficient: a higher layer can repeat the same binding
// and later shadow a copy-on-write rebind made in the entry's lower owner.
func (config *Config) contextCloudBindingOwnerIdentity(contextName string) (string, error) {
	var identity string
	for _, source := range config.Sources {
		raw, err := decodeLoginMutationSource(source)
		if err != nil {
			return "", err
		}
		ctx := raw.Contexts[contextName]
		if ctx == nil || ctx.Cloud == "" {
			continue
		}
		identity, err = canonicalConfigSourceForLayer(source.Path, source.Type)
		if err != nil {
			return "", fmt.Errorf("resolve Cloud binding owner %s: %w", source.Path, err)
		}
	}
	return identity, nil
}

func (config *Config) sourceForIdentity(identity string) (ConfigSource, bool) {
	for _, source := range config.Sources {
		canonical, err := canonicalConfigSourceForLayer(source.Path, source.Type)
		if err == nil && canonical == identity {
			return source, true
		}
	}
	return ConfigSource{}, false
}

func sourceContainsContextBindings(
	source ConfigSource,
	contextName string,
	effective *Context,
	intent LoginMutationIntent,
) (bool, error) {
	raw, err := decodeLoginMutationSource(source)
	if err != nil {
		return false, err
	}
	rawCtx := raw.Contexts[contextName]
	if rawCtx == nil {
		return false, nil
	}
	if intent == LoginMutationCloud {
		// Cloud-only login can safely retain a stack binding inherited from
		// another layer: it never mutates context.stack or the stack entry.
		return rawCtx.Cloud == effective.Cloud, nil
	}
	return rawCtx.Stack == effective.Stack && rawCtx.Cloud == effective.Cloud, nil
}

func decodeLoginMutationSource(source ConfigSource) (Config, error) {
	contents := source.snapshot
	if contents == nil {
		var err error
		contents, err = readConfigSource(source)
		if err != nil {
			return Config{}, fmt.Errorf("read candidate config %s: %w", source.Path, err)
		}
	}

	var raw Config
	codec := &format.YAMLCodec{BytesAsBase64: true}
	if err := codec.Decode(bytes.NewReader(contents), &raw); err != nil {
		return Config{}, fmt.Errorf("decode candidate config %s: %w", source.Path, err)
	}
	return raw, nil
}

// VerifyLoginMutationBindings rechecks the selected raw owner immediately
// before authentication begins. Planning uses a layered-load snapshot; this
// second check closes the gap where the owner file changes between that load
// and the raw owner reload. The eventual Write CAS protects the later
// authentication-to-persistence interval.
func VerifyLoginMutationBindings(
	target ConfigSource,
	contextName string,
	effective, persisted *Context,
	intent LoginMutationIntent,
) error {
	if effective != nil && persisted != nil {
		if intent == LoginMutationCloud && effective.Cloud == persisted.Cloud {
			return nil
		}
		if intent == LoginMutationUnified &&
			effective.Stack == persisted.Stack && effective.Cloud == persisted.Cloud {
			return nil
		}
	}

	wantStack, wantCloud := "", ""
	if effective != nil {
		wantStack, wantCloud = effective.Stack, effective.Cloud
	}
	gotStack, gotCloud := "", ""
	if persisted != nil {
		gotStack, gotCloud = persisted.Stack, persisted.Cloud
	}
	return gcxerrors.DetailedError{
		Summary: "Configuration changed during login planning",
		Details: fmt.Sprintf(
			"Context %q in %s now has stack/cloud bindings %q / %q; the effective layered snapshot used %q / %q. No authentication request was sent.",
			contextName,
			target.Path,
			gotStack,
			gotCloud,
			wantStack,
			wantCloud,
		),
		Suggestions: []string{
			"Review the changed config and retry",
			"Or re-run with --config " + strconv.Quote(target.Path) + " to select this owner explicitly",
		},
	}
}

// LoginCloudMutationSafety snapshots shared-reference and name-collision
// evidence from the complete layered view before login reloads a single raw
// owner for persistence.
func (config *Config) LoginCloudMutationSafety(contextName string, target ConfigSource) (CloudMutationSafety, error) {
	safety := CloudMutationSafety{ReservedEntryNames: make([]string, 0, len(config.Cloud))}
	targetIdentity, err := canonicalConfigSourceForLayer(target.Path, target.Type)
	if err != nil {
		return CloudMutationSafety{}, fmt.Errorf("identify Cloud login mutation owner %s: %w", target.Path, err)
	}
	for name, entry := range config.Cloud {
		safety.ReservedEntryNames = append(safety.ReservedEntryNames, name)
		if entry == nil || entry.sourceIdentity != targetIdentity {
			safety.ForeignEntryNames = append(safety.ForeignEntryNames, name)
		}
	}
	slices.Sort(safety.ReservedEntryNames)
	slices.Sort(safety.ForeignEntryNames)

	targetContext := config.Contexts[contextName]
	if targetContext == nil || targetContext.Cloud == "" {
		return safety, nil
	}
	for name, candidate := range config.Contexts {
		if name != contextName && candidate != nil && candidate.Cloud == targetContext.Cloud {
			safety.SharedInEffectiveConfig = true
			break
		}
	}
	return safety, nil
}

func (config *Config) loginMutationTargetError(contextName, details string) error {
	suggestions := make([]string, 0, len(config.Sources)+1)
	for _, source := range config.Sources {
		suggestions = append(suggestions, "Review the owner, then re-run with --config "+strconv.Quote(source.Path))
	}
	if contextName != "" {
		suggestions = append(suggestions, fmt.Sprintf("Keep stack and Cloud bindings for context %q together in one config file", contextName))
	}
	return gcxerrors.DetailedError{
		Summary:     "Configuration write target is ambiguous",
		Details:     details,
		Suggestions: suggestions,
	}
}
