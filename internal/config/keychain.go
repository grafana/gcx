package config

import (
	"errors"
	"sync"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/grafana-app-sdk/logging"
)

// lazyStore defers opening the keychain backend until the first Get/Set/Delete.
// resolveSentinelsForOwner only calls Get for fields that actually hold a
// sentinel, and migratePlaintextSecrets only calls Set when plaintext secrets
// are present, so a config whose current context has no keychain-backed secrets
// never probes the OS keychain at all. Once opened, the backend is reused.
type lazyStore struct {
	once    sync.Once
	open    func() credentials.Store
	backend credentials.Store
}

func newLazyStore(open func() credentials.Store) *lazyStore {
	return &lazyStore{open: open}
}

func (l *lazyStore) resolve() credentials.Store {
	l.once.Do(func() { l.backend = l.open() })
	return l.backend
}

func (l *lazyStore) Get(key string) (string, error) { return l.resolve().Get(key) }
func (l *lazyStore) Set(key, value string) error    { return l.resolve().Set(key, value) }
func (l *lazyStore) Delete(key string) error        { return l.resolve().Delete(key) }

// keychainBacked tracks which (owner, field) pairs were stored in the keychain
// at load time. Owners are "stack:<name>" / "cloud:<name>" keys. The map lives
// on Config as an unexported field; it is populated during Load and consumed by
// reconcileKeychain during Write to round-trip sentinel values to disk.
type keychainBacked map[string]map[credentials.Field]bool

func (k keychainBacked) mark(owner string, field credentials.Field) {
	if k[owner] == nil {
		k[owner] = make(map[credentials.Field]bool)
	}
	k[owner][field] = true
}

// keychainPreserved tracks (owner, field) pairs whose sentinel could not be
// resolved at load time (keychain unavailable), mapped to the original
// sentinel string. Write round-trips the original verbatim — it may be a
// legacy-format sentinel whose value lives under a legacy account key, so it
// must not be re-derived from the owner.
type keychainPreserved map[string]map[credentials.Field]string

func (k keychainPreserved) mark(owner string, field credentials.Field, sentinel string) {
	if k[owner] == nil {
		k[owner] = make(map[credentials.Field]string)
	}
	k[owner][field] = sentinel
}

// secretRef is a get/set handle for a secret field. Provider-map secrets
// cannot be addressed by *string (Go map values are not addressable), so all
// callers go through this interface uniformly.
type secretRef struct {
	get func() string
	set func(string)
}

// secretOwner identifies one keychain owner — a stack entry or a cloud auth
// entry — together with the secret fields it may hold.
type secretOwner struct {
	key    string // account-key prefix, e.g. "stack:prod" or "cloud:grafana-com"
	fields []credentials.Field
	ref    func(field credentials.Field) (secretRef, bool)
}

//nolint:gochecknoglobals // constant-like lookup lists; never mutated.
var (
	stackSecretFields = []credentials.Field{
		credentials.FieldGrafanaToken,
		credentials.FieldGrafanaPassword,
		credentials.FieldOAuthToken,
		credentials.FieldOAuthRefreshToken,
		credentials.FieldSMToken,
	}
	cloudSecretFields = []credentials.Field{
		credentials.FieldCloudToken,
		credentials.FieldOAuthToken,
	}
)

func stackOwner(name string, stack *StackConfig) secretOwner {
	return secretOwner{
		key:    credentials.StackOwner(name),
		fields: stackSecretFields,
		ref:    func(field credentials.Field) (secretRef, bool) { return stackFieldRef(stack, field) },
	}
}

func cloudOwner(name string, entry *CloudEntry) secretOwner {
	return secretOwner{
		key:    credentials.CloudOwner(name),
		fields: cloudSecretFields,
		ref:    func(field credentials.Field) (secretRef, bool) { return cloudFieldRef(entry, field) },
	}
}

// secretOwners enumerates every keychain owner in the config, referenced by a
// context or not, so reconcile and plaintext migration cover orphaned entries.
func (cfg *Config) secretOwners() []secretOwner {
	owners := make([]secretOwner, 0, len(cfg.Stacks)+len(cfg.Cloud))
	for name, stack := range cfg.Stacks {
		if stack != nil {
			owners = append(owners, stackOwner(name, stack))
		}
	}
	for name, entry := range cfg.Cloud {
		if entry != nil {
			owners = append(owners, cloudOwner(name, entry))
		}
	}
	return owners
}

// contextOwners returns the owners referenced by a single context.
func contextOwners(ctx *Context) []secretOwner {
	var owners []secretOwner
	if ctx.StackEntry != nil {
		owners = append(owners, stackOwner(ctx.Stack, ctx.StackEntry))
	}
	if ctx.CloudEntry != nil {
		owners = append(owners, cloudOwner(ctx.Cloud, ctx.CloudEntry))
	}
	return owners
}

// stackFieldRef returns a get/set handle for the named secret on a stack
// entry, or zero-value (ok=false) if the field's parent struct/map is absent.
func stackFieldRef(stack *StackConfig, field credentials.Field) (secretRef, bool) {
	switch field {
	case credentials.FieldGrafanaToken:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.APIToken },
			set: func(v string) { stack.Grafana.APIToken = v },
		}, true
	case credentials.FieldGrafanaPassword:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.Password },
			set: func(v string) { stack.Grafana.Password = v },
		}, true
	case credentials.FieldOAuthToken:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.OAuthToken },
			set: func(v string) { stack.Grafana.OAuthToken = v },
		}, true
	case credentials.FieldOAuthRefreshToken:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.OAuthRefreshToken },
			set: func(v string) { stack.Grafana.OAuthRefreshToken = v },
		}, true
	case credentials.FieldSMToken:
		return providerFieldRef(stack, "synth", "sm-token")
	case credentials.FieldCloudToken:
	}
	return secretRef{}, false
}

// cloudFieldRef returns a get/set handle for the named secret on a cloud auth
// entry.
func cloudFieldRef(entry *CloudEntry, field credentials.Field) (secretRef, bool) {
	switch field {
	case credentials.FieldCloudToken:
		return secretRef{
			get: func() string { return entry.Token },
			set: func(v string) { entry.Token = v },
		}, true
	case credentials.FieldOAuthToken:
		return secretRef{
			get: func() string { return entry.OAuthToken },
			set: func(v string) { entry.OAuthToken = v },
		}, true
	case credentials.FieldGrafanaToken, credentials.FieldGrafanaPassword,
		credentials.FieldOAuthRefreshToken, credentials.FieldSMToken:
	}
	return secretRef{}, false
}

// providerFieldRef returns a get/set handle for stack.Providers[provider][key],
// or zero-value (ok=false) if the provider sub-map has no entry for key.
// The setter creates the parent map on first write so a migration round-trip
// can re-substitute the sentinel value during Write.
func providerFieldRef(stack *StackConfig, provider, key string) (secretRef, bool) {
	if stack.Providers == nil || stack.Providers[provider] == nil {
		return secretRef{}, false
	}
	if _, present := stack.Providers[provider][key]; !present {
		return secretRef{}, false
	}
	return secretRef{
		get: func() string { return stack.Providers[provider][key] },
		set: func(v string) {
			if stack.Providers == nil {
				stack.Providers = map[string]map[string]string{}
			}
			if stack.Providers[provider] == nil {
				stack.Providers[provider] = map[string]string{}
			}
			stack.Providers[provider][key] = v
		},
	}, true
}

// resolveSentinelsForOwner replaces keychain sentinels with their plaintext
// values from the store for a single owner. Lookups use the account key
// embedded in the sentinel itself, NOT one derived from the owner: configs
// migrated from the legacy format carry legacy per-context sentinels in their
// new homes, and those must keep resolving. Successful resolutions are marked
// under the canonical owner key so the next Write re-keys them.
//
// It returns two maps: backed lists the (owner, field) pairs that resolved
// successfully, and preserve lists pairs whose lookup failed because the
// keychain was unavailable, mapped to their original sentinel string. In both
// failure cases the in-memory value is cleared so the command surfaces a
// missing credential rather than sending a sentinel string as one.
func resolveSentinelsForOwner(owner secretOwner, store credentials.Store) (keychainBacked, keychainPreserved) {
	backed, preserve := keychainBacked{}, keychainPreserved{}
	for _, field := range owner.fields {
		ref, ok := owner.ref(field)
		if !ok {
			continue
		}
		cur := ref.get()
		if !credentials.IsSentinel(cur) {
			continue
		}
		parsedOwner, parsedField, ok := credentials.ParseSentinel(cur)
		if !ok {
			ref.set("")
			continue
		}
		value, err := store.Get(credentials.AccountKey(parsedOwner, parsedField))
		if err != nil {
			ref.set("")
			if errors.Is(err, credentials.ErrNotFound) {
				continue
			}
			preserve.mark(owner.key, field, cur)
			continue
		}
		ref.set(value)
		backed.mark(owner.key, field)
	}
	return backed, preserve
}

// resolveSentinelsForContext resolves keychain sentinels on the stack and
// cloud entries referenced by a single context. Idempotent: already-resolved
// fields hold plaintext and are skipped.
func resolveSentinelsForContext(ctx *Context, store credentials.Store) (keychainBacked, keychainPreserved) {
	backed, preserve := keychainBacked{}, keychainPreserved{}
	for _, owner := range contextOwners(ctx) {
		b, p := resolveSentinelsForOwner(owner, store)
		for ownerKey, fields := range b {
			for field := range fields {
				backed.mark(ownerKey, field)
			}
		}
		for ownerKey, fields := range p {
			for field, sentinel := range fields {
				preserve.mark(ownerKey, field, sentinel)
			}
		}
	}
	return backed, preserve
}

// migratePlaintextSecrets pushes any plaintext secret values into the store
// and marks them as keychain-backed. Returns the number of fields migrated.
// If the store is unavailable, it emits a one-time warning and returns 0.
func migratePlaintextSecrets(cfg *Config, store credentials.Store, log logging.Logger) int {
	migrated := 0
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			ref, ok := owner.ref(field)
			if !ok {
				continue
			}
			cur := ref.get()
			if cur == "" || credentials.IsSentinel(cur) {
				continue
			}
			if cfg.keychainFields[owner.key][field] {
				continue
			}
			if err := store.Set(credentials.AccountKey(owner.key, field), cur); err != nil {
				if errors.Is(err, credentials.ErrUnavailable) {
					credentials.WarnUnavailableOnce(func() {
						log.Warn("keychain unavailable; credentials remain in plaintext on disk",
							"hint", "install or unlock your OS keychain to enable encrypted credential storage")
					})
					return migrated
				}
				log.Warn("could not write keychain entry",
					"owner", owner.key,
					"field", string(field),
					"error", err.Error())
				continue
			}
			if cfg.keychainFields == nil {
				cfg.keychainFields = keychainBacked{}
			}
			cfg.keychainFields.mark(owner.key, field)
			migrated++
		}
	}
	return migrated
}

// hasSecretsToReconcile reports whether Write needs to touch the keychain at
// all. It is true when any secret field holds a value (so it must be written
// through), or when a field is known to be keychain-backed or preserved (so it
// may need a sentinel round-trip or a stale-entry delete). When false, Write
// skips opening the keychain entirely, so secret-less config writes never probe
// the OS backend.
func (cfg *Config) hasSecretsToReconcile() bool {
	if len(cfg.keychainFields) > 0 || len(cfg.keychainPreserve) > 0 {
		return true
	}
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			if ref, ok := owner.ref(field); ok && ref.get() != "" {
				return true
			}
		}
	}
	return false
}

// reconcileKeychain walks every secret field and brings the keychain and the
// in-memory config into agreement for a single YAML encode, returning a restore
// function that reverts the in-memory swaps afterwards. For each field it:
//
//   - preserves an unresolvable sentinel (keychain was unavailable at load) by
//     writing the original string back verbatim, never touching the store;
//   - deletes the keychain entry for a field that was backed but is now empty
//     (gcx config unset, or an auth-method switch that drops the credential) —
//     only under the canonical owner key, so legacy per-context entries backing
//     a pre-migration config or its .legacy.bak are never garbage-collected;
//   - writes any plaintext secret through to the keychain and substitutes a
//     sentinel for the on-disk value, covering migration, re-keying of resolved
//     legacy sentinels, and freshly written secrets from gcx login / gcx config
//     set.
//
// If the keychain is unavailable, plaintext secrets are left in place with a
// one-time warning so gcx still works on headless or locked boxes.
func reconcileKeychain(cfg *Config, store credentials.Store, log logging.Logger) func() {
	if cfg.keychainFields == nil {
		cfg.keychainFields = keychainBacked{}
	}
	type swap struct {
		ref       secretRef
		plaintext string
	}
	var swaps []swap
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			ref, ok := owner.ref(field)
			if !ok {
				continue
			}
			key := credentials.AccountKey(owner.key, field)

			// Unresolvable at load: round-trip the original sentinel verbatim.
			if sentinel := cfg.keychainPreserve[owner.key][field]; sentinel != "" {
				swaps = append(swaps, swap{ref: ref, plaintext: ref.get()})
				ref.set(sentinel)
				continue
			}

			cur := ref.get()

			if cur == "" {
				// Field cleared. If it was keychain-backed, remove the now-stale
				// entry instead of orphaning it.
				if cfg.keychainFields[owner.key][field] {
					if err := store.Delete(key); err != nil && !errors.Is(err, credentials.ErrUnavailable) {
						log.Warn("could not remove stale keychain entry",
							"owner", owner.key,
							"field", string(field),
							"error", err.Error())
					}
					delete(cfg.keychainFields[owner.key], field)
				}
				continue
			}

			if credentials.IsSentinel(cur) {
				continue
			}

			// Plaintext secret: write it through to the keychain and substitute
			// a sentinel for the on-disk value.
			if err := store.Set(key, cur); err != nil {
				if errors.Is(err, credentials.ErrUnavailable) {
					credentials.WarnUnavailableOnce(func() {
						log.Warn("keychain unavailable; credentials remain in plaintext on disk",
							"hint", "install or unlock your OS keychain to enable encrypted credential storage")
					})
					continue
				}
				log.Warn("could not write keychain entry",
					"owner", owner.key,
					"field", string(field),
					"error", err.Error())
				continue
			}
			cfg.keychainFields.mark(owner.key, field)
			swaps = append(swaps, swap{ref: ref, plaintext: cur})
			ref.set(credentials.FormatSentinel(owner.key, field))
		}
	}
	return func() {
		for _, s := range swaps {
			s.ref.set(s.plaintext)
		}
	}
}
