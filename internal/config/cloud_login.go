package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/gcxerrors"
)

// MergeCloudInto applies non-empty fields from incoming onto existing,
// allocating existing if nil. It returns the merged entry.
//
// An entry holds one credential: setting a token clears the OAuth fields and
// vice versa, so switching auth methods leaves no stale credential behind
// (mirroring how gcx login switches Grafana auth methods).
func MergeCloudInto(existing, incoming *CloudEntry) *CloudEntry {
	if existing == nil {
		existing = &CloudEntry{}
	}
	if incoming.Token != "" {
		existing.Token = incoming.Token
		existing.OAuthToken = ""
		existing.OAuthTokenExpiresAt = ""
		existing.OAuthScopes = nil
	}
	if incoming.OAuthToken != "" {
		existing.OAuthToken = incoming.OAuthToken
		existing.OAuthTokenExpiresAt = incoming.OAuthTokenExpiresAt
		existing.OAuthScopes = canonicalOAuthScopes(incoming.OAuthScopes)
		existing.Token = ""
	}
	if incoming.OAuthUrl != "" {
		existing.OAuthUrl = incoming.OAuthUrl
	}
	if incoming.APIUrl != "" {
		existing.APIUrl = incoming.APIUrl
	}
	return existing
}

// EnsureCloudEntry applies entry for contextName and returns the entry name the
// context should bind. An entry referenced by multiple contexts is immutable by
// default: changing any credential, OAuth metadata, or endpoint performs a
// copy-on-write and leaves the shared entry untouched. An entry referenced only
// by the target context is updated in place. Host-name and copy-on-write name
// collisions are allocated safely, while exact matches are reused.
func (config *Config) EnsureCloudEntry(existingRef string, entry CloudEntry, contextName string) string {
	return config.EnsureCloudEntryWithSafety(existingRef, entry, contextName, CloudMutationSafety{})
}

// CloudMutationSafety carries evidence from the complete layered view into a
// raw-owner write. A raw file cannot count contexts, distinguish foreign
// same-named entries, or see name collisions contributed by other layers, so
// login must preserve that evidence across the reload rather than infer safety
// from the write target alone.
type CloudMutationSafety struct {
	SharedInEffectiveConfig bool
	ReservedEntryNames      []string
	ForeignEntryNames       []string
}

// EnsureCloudEntryWithSafety is EnsureCloudEntry with cross-layer sharing
// evidence. When the current entry is referenced outside the raw owner, a
// changed credential/destination is always written to a fresh name that is not
// present anywhere in the effective layered configuration.
func (config *Config) EnsureCloudEntryWithSafety(
	existingRef string,
	entry CloudEntry,
	contextName string,
	safety CloudMutationSafety,
) string {
	// The target may not be the file's current context, whose sentinels Load
	// resolved eagerly. Resolve it before comparing or cloning its entry so a new
	// entry never inherits a sentinel owned by a different name.
	config.ResolveContext(contextName)

	if existingRef == "" {
		return config.ensureUnboundCloudEntry(entry, contextName, safety)
	}
	return config.ensureBoundCloudEntry(existingRef, entry, contextName, safety)
}

func (config *Config) ensureUnboundCloudEntry(entry CloudEntry, contextName string, safety CloudMutationSafety) string {
	base := cloudEntryName(entry.APIUrl)
	if slices.Contains(safety.ForeignEntryNames, base) {
		name := config.availableIsolatedCloudEntryName(base+"-"+contextName, &entry, safety.ReservedEntryNames)
		config.setCloudAuthEntry(name, entry)
		return name
	}
	if existing := config.Cloud[base]; existing != nil {
		desired := mergedCloudEntry(existing, &entry)
		if sameCloudEntry(existing, &desired, config.keychainStore) {
			return base
		}
		name := config.availableIsolatedCloudEntryName(base+"-"+contextName, &desired, safety.ReservedEntryNames)
		if config.Cloud[name] == nil {
			config.setCloudAuthEntry(name, desired)
		}
		return name
	}
	if slices.Contains(safety.ReservedEntryNames, base) {
		name := config.availableIsolatedCloudEntryName(base+"-"+contextName, &entry, safety.ReservedEntryNames)
		config.setCloudAuthEntry(name, entry)
		return name
	}

	config.setCloudAuthEntry(base, entry)
	return base
}

func (config *Config) ensureBoundCloudEntry(
	existingRef string,
	entry CloudEntry,
	contextName string,
	safety CloudMutationSafety,
) string {
	existing := config.Cloud[existingRef]
	if existing == nil {
		config.setCloudAuthEntry(existingRef, entry)
		return existingRef
	}

	desired := mergedCloudEntry(existing, &entry)
	if sameCloudEntry(existing, &desired, config.keychainStore) {
		return existingRef
	}
	if safety.SharedInEffectiveConfig {
		name := config.availableIsolatedCloudEntryName(existingRef+"-"+contextName, &desired, safety.ReservedEntryNames)
		config.setCloudAuthEntry(name, desired)
		return name
	}
	if config.cloudEntryRefCount(existingRef) <= 1 {
		config.setCloudAuthEntry(existingRef, desired)
		return existingRef
	}

	name := config.availableCloudEntryName(existingRef+"-"+contextName, &desired)
	if config.Cloud[name] == nil {
		config.setCloudAuthEntry(name, desired)
	}
	return name
}

func (config *Config) availableIsolatedCloudEntryName(base string, desired *CloudEntry, reserved []string) string {
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		if slices.Contains(reserved, name) {
			continue
		}
		existing := config.Cloud[name]
		if existing == nil || sameCloudEntry(existing, desired, config.keychainStore) {
			return name
		}
	}
}

// setCloudAuthEntry records explicit set/unset intent for both mutually
// exclusive credential fields. This matters when an unavailable keychain
// reference was cleared in memory: value comparison alone cannot distinguish
// an intentional auth switch from an untouched, temporarily unavailable value.
func (config *Config) setCloudAuthEntry(name string, entry CloudEntry) {
	entry.OAuthScopes = canonicalOAuthScopes(entry.OAuthScopes)
	config.SetCloudEntry(name, entry)
	owner := credentials.CloudOwner(name)
	config.MarkSecretMutation(owner, credentials.FieldCloudToken)
	config.MarkSecretMutation(owner, credentials.FieldOAuthToken)
}

func mergedCloudEntry(existing, incoming *CloudEntry) CloudEntry {
	merged := *existing
	merged.OAuthScopes = append([]string(nil), existing.OAuthScopes...)
	return *MergeCloudInto(&merged, incoming)
}

// sameCloudEntry compares the complete credential/destination tuple. Existing
// may still contain keychain sentinels, so compare against a resolved copy.
// Resolution failures compare different, choosing isolation over mutation.
func sameCloudEntry(existing, desired *CloudEntry, store credentials.Store) bool {
	resolved := *existing
	resolved.OAuthScopes = append([]string(nil), existing.OAuthScopes...)
	if store != nil {
		resolveSentinelsForOwner(cloudOwner(existing.Name, &resolved), store)
	}
	return resolved.Token == desired.Token &&
		resolved.OAuthToken == desired.OAuthToken &&
		resolved.OAuthTokenExpiresAt == desired.OAuthTokenExpiresAt &&
		slices.Equal(canonicalOAuthScopes(resolved.OAuthScopes), canonicalOAuthScopes(desired.OAuthScopes)) &&
		resolved.OAuthUrl == desired.OAuthUrl &&
		resolved.APIUrl == desired.APIUrl
}

func canonicalOAuthScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	canonical := append([]string(nil), scopes...)
	slices.Sort(canonical)
	return slices.Compact(canonical)
}

func (config *Config) cloudEntryRefCount(name string) int {
	count := 0
	for _, cfgCtx := range config.Contexts {
		if cfgCtx != nil && cfgCtx.Cloud == name {
			count++
		}
	}
	return count
}

// availableCloudEntryName returns base when free, reuses it when it already
// contains the desired tuple, or appends a numeric suffix until one is safe.
func (config *Config) availableCloudEntryName(base string, desired *CloudEntry) string {
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		existing := config.Cloud[name]
		if existing == nil || sameCloudEntry(existing, desired, config.keychainStore) {
			return name
		}
	}
}

// ResolveContextName picks the context to operate on: the explicit override when
// set, otherwise the config's current context, falling back to the default.
func ResolveContextName(override string, cfg Config) string {
	if override != "" {
		return override
	}
	if cfg.CurrentContext != "" {
		return cfg.CurrentContext
	}
	return DefaultContextName
}

// SaveCloudConfig writes cloud credentials into a named cloud entry and binds
// it to the resolved context (see ResolveContextName), creating the context
// if it doesn't exist. The entry is the one the context already references,
// or one named after the API URL host otherwise, so re-authenticating
// refreshes credentials in place. Returns the context and entry names.
func SaveCloudConfig(ctx context.Context, source Source, contextOverride string, entry *CloudEntry) (string, string, error) {
	return SaveCloudConfigWithSafety(ctx, source, contextOverride, entry, CloudMutationSafety{})
}

// SaveCloudConfigWithSafety preserves cross-layer shared-entry evidence while
// loading and writing only the selected raw owner.
func SaveCloudConfigWithSafety(
	ctx context.Context,
	source Source,
	contextOverride string,
	entry *CloudEntry,
	safety CloudMutationSafety,
) (string, string, error) {
	return SaveCloudConfigGuarded(ctx, source, contextOverride, entry, safety, LoginMutationGuard{})
}

// SaveCloudConfigGuarded extends SaveCloudConfigWithSafety across the
// authentication interval by rejecting a post-auth reload whose raw source no
// longer matches the pre-auth revision.
func SaveCloudConfigGuarded(
	ctx context.Context,
	source Source,
	contextOverride string,
	entry *CloudEntry,
	safety CloudMutationSafety,
	guard LoginMutationGuard,
) (string, string, error) {
	cfg, err := LoadLoginMutationGuarded(ctx, source, guard)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", &gcxerrors.DetailedError{
			Summary: "Failed to load config",
			Parent:  err,
			Suggestions: []string{
				"Check your config file syntax: gcx config edit",
				"Or reset with: rm ~/.config/gcx/config.yaml && gcx cloud login",
			},
		}
	}
	// Keep the source-absent revision marker returned by Load. Write uses it to
	// avoid replacing a config another process creates while login is running.

	contextName := ResolveContextName(contextOverride, cfg)

	if !cfg.HasContext(contextName) {
		cfg.SetContext(contextName, true, Context{})
	}
	curCtx := cfg.Contexts[contextName]

	// Merge the incoming auth fields onto the existing entry so
	// re-authenticating refreshes credentials without dropping other fields.
	entryName := cfg.EnsureCloudEntryWithSafety(curCtx.Cloud, *entry, contextName, safety)
	curCtx.Cloud = entryName
	cfg.Resolve()
	if err := guard.VerifyCurrentSources(); err != nil {
		return "", "", err
	}

	if err := Write(ctx, source, cfg); err != nil {
		return "", "", &gcxerrors.DetailedError{
			Summary: "Failed to save config",
			Parent:  err,
			Suggestions: []string{
				"Check file permissions on the config file",
				"Try: gcx config edit",
			},
		}
	}

	return contextName, entryName, nil
}
