package config

import (
	"context"
	"errors"
	"os"

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
	}
	if incoming.OAuthToken != "" {
		existing.OAuthToken = incoming.OAuthToken
		existing.OAuthTokenExpiresAt = incoming.OAuthTokenExpiresAt
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

// EnsureCloudEntry merges entry into the named entry when existingRef is set,
// otherwise into an entry named after the entry's API URL host, and returns
// the entry name used. The caller binds the returned name to a context.
//
// When the host-named entry already exists with a different credential (e.g.
// an org-wide CAP is bound to other contexts and this login carries a
// stack-scoped one), the new entry is suffixed with the context name instead
// of quietly replacing the shared credential — the same collision convention
// migration uses. Same-credential logins still dedup onto the shared entry.
func (config *Config) EnsureCloudEntry(existingRef string, entry CloudEntry, contextName string) string {
	name := existingRef
	if name == "" {
		name = cloudEntryName(entry.APIUrl)
		if existing := config.Cloud[name]; existing != nil && !sameCloudCredential(existing, &entry, config.keychainStore) {
			name += "-" + contextName
		}
	}
	config.SetCloudEntry(name, *MergeCloudInto(config.Cloud[name], &entry))
	return name
}

// sameCloudCredential reports whether incoming carries the same credential as
// existing. The existing entry may hold unresolved keychain sentinels (it can
// be referenced only by non-current contexts, which resolve lazily), so those
// are resolved for the comparison. Unresolvable values compare as different —
// failing toward a new entry rather than replacing a shared credential.
func sameCloudCredential(existing, incoming *CloudEntry, store credentials.Store) bool {
	if store != nil {
		resolved := *existing
		resolveSentinelsForOwner(cloudOwner(existing.Name, &resolved), store)
		existing = &resolved
	}
	if incoming.Token != "" {
		return existing.Token == incoming.Token
	}
	if incoming.OAuthToken != "" {
		return existing.OAuthToken == incoming.OAuthToken
	}
	return false
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
	cfg, err := Load(ctx, source)
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
	if errors.Is(err, os.ErrNotExist) {
		cfg = Config{}
	}

	contextName := ResolveContextName(contextOverride, cfg)

	if !cfg.HasContext(contextName) {
		cfg.SetContext(contextName, true, Context{})
	}
	curCtx := cfg.Contexts[contextName]

	// Merge the incoming auth fields onto the existing entry so
	// re-authenticating refreshes credentials without dropping other fields.
	entryName := cfg.EnsureCloudEntry(curCtx.Cloud, *entry, contextName)
	curCtx.Cloud = entryName
	cfg.Resolve()

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
