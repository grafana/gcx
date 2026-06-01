package config

import (
	"errors"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/grafana-app-sdk/logging"
)

// keychainBacked tracks which (context, field) pairs were stored in the
// keychain at load time. The map lives on Config as an unexported field; it is
// populated by resolveSentinels during Load and consumed by reconcileKeychain
// during Write to round-trip sentinel values to disk.
type keychainBacked map[string]map[credentials.Field]bool

func (k keychainBacked) mark(ctx string, field credentials.Field) {
	if k[ctx] == nil {
		k[ctx] = make(map[credentials.Field]bool)
	}
	k[ctx][field] = true
}

// secretRef is a get/set handle for a secret field. Provider-map secrets
// cannot be addressed by *string (Go map values are not addressable), so all
// callers go through this interface uniformly.
type secretRef struct {
	get func() string
	set func(string)
}

// fieldRef returns a get/set handle for the named secret on ctx, or zero-value
// (ok=false) if the field's parent struct/map is not present.
func fieldRef(ctx *Context, field credentials.Field) (secretRef, bool) {
	switch field {
	case credentials.FieldCloudToken:
		if ctx.Cloud == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return ctx.Cloud.Token },
			set: func(v string) { ctx.Cloud.Token = v },
		}, true
	case credentials.FieldGrafanaToken:
		if ctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return ctx.Grafana.APIToken },
			set: func(v string) { ctx.Grafana.APIToken = v },
		}, true
	case credentials.FieldGrafanaPassword:
		if ctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return ctx.Grafana.Password },
			set: func(v string) { ctx.Grafana.Password = v },
		}, true
	case credentials.FieldOAuthToken:
		if ctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return ctx.Grafana.OAuthToken },
			set: func(v string) { ctx.Grafana.OAuthToken = v },
		}, true
	case credentials.FieldOAuthRefreshToken:
		if ctx.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return ctx.Grafana.OAuthRefreshToken },
			set: func(v string) { ctx.Grafana.OAuthRefreshToken = v },
		}, true
	case credentials.FieldSMToken:
		return providerFieldRef(ctx, "synth", "sm-token")
	}
	return secretRef{}, false
}

// providerFieldRef returns a get/set handle for ctx.Providers[provider][key],
// or zero-value (ok=false) if the provider sub-map has no entry for key.
// The setter creates the parent map on first write so a migration round-trip
// can re-substitute the sentinel value during Write.
func providerFieldRef(ctx *Context, provider, key string) (secretRef, bool) {
	if ctx.Providers == nil || ctx.Providers[provider] == nil {
		return secretRef{}, false
	}
	if _, present := ctx.Providers[provider][key]; !present {
		return secretRef{}, false
	}
	return secretRef{
		get: func() string { return ctx.Providers[provider][key] },
		set: func(v string) {
			if ctx.Providers == nil {
				ctx.Providers = map[string]map[string]string{}
			}
			if ctx.Providers[provider] == nil {
				ctx.Providers[provider] = map[string]string{}
			}
			ctx.Providers[provider][key] = v
		},
	}, true
}

// resolveSentinels walks every context and replaces keychain sentinels with
// their plaintext values from the store. It returns two maps: backed lists the
// (context, field) pairs that resolved successfully (so Write can re-substitute
// sentinels), and preserve lists pairs whose lookup failed because the keychain
// was unavailable. In both failure cases the in-memory value is cleared so the
// command surfaces a missing credential rather than sending a sentinel string
// as one. The distinction matters on Write: a malformed or genuinely-absent
// reference is dropped, but an unresolvable-because-unavailable one is
// round-tripped back to disk verbatim so a transient outage never destroys it.
func resolveSentinels(cfg *Config, store credentials.Store, log logging.Logger) (keychainBacked, keychainBacked) {
	backed, preserve := keychainBacked{}, keychainBacked{}
	for ctxName, ctx := range cfg.Contexts {
		if ctx == nil {
			continue
		}
		for _, field := range credentials.AllFields {
			ref, ok := fieldRef(ctx, field)
			if !ok {
				continue
			}
			cur := ref.get()
			if !credentials.IsSentinel(cur) {
				continue
			}
			parsedCtx, parsedField, ok := credentials.ParseSentinel(cur)
			if !ok || parsedCtx != ctxName || parsedField != field {
				log.Warn("ignoring malformed keychain sentinel",
					"context", ctxName,
					"field", string(field),
					"value", cur)
				ref.set("")
				continue
			}
			value, err := store.Get(credentials.AccountKey(ctxName, field))
			if err != nil {
				ref.set("")
				if errors.Is(err, credentials.ErrNotFound) {
					// The entry is genuinely gone; drop the dangling reference.
					log.Warn("keychain entry not found; dropping dangling reference",
						"context", ctxName,
						"field", string(field))
					continue
				}
				// Keychain unavailable or another transient error: keep the
				// on-disk sentinel intact by preserving it for Write.
				log.Warn("could not resolve keychain entry; leaving reference in place",
					"context", ctxName,
					"field", string(field),
					"error", err.Error())
				preserve.mark(ctxName, field)
				continue
			}
			ref.set(value)
			backed.mark(ctxName, field)
		}
	}
	return backed, preserve
}

// migratePlaintextSecrets pushes any plaintext secret values into the store
// and marks them as keychain-backed. Returns the number of fields migrated.
// If the store is unavailable, it emits a one-time warning and returns 0.
func migratePlaintextSecrets(cfg *Config, store credentials.Store, log logging.Logger) int {
	migrated := 0
	for ctxName, ctx := range cfg.Contexts {
		if ctx == nil {
			continue
		}
		for _, field := range credentials.AllFields {
			ref, ok := fieldRef(ctx, field)
			if !ok {
				continue
			}
			cur := ref.get()
			if cur == "" || credentials.IsSentinel(cur) {
				continue
			}
			if cfg.keychainFields[ctxName][field] {
				continue
			}
			if err := store.Set(credentials.AccountKey(ctxName, field), cur); err != nil {
				if errors.Is(err, credentials.ErrUnavailable) {
					credentials.WarnUnavailableOnce(func() {
						log.Warn("keychain unavailable; credentials remain in plaintext on disk",
							"hint", "install or unlock your OS keychain to enable encrypted credential storage")
					})
					return migrated
				}
				log.Warn("could not write keychain entry",
					"context", ctxName,
					"field", string(field),
					"error", err.Error())
				continue
			}
			if cfg.keychainFields == nil {
				cfg.keychainFields = keychainBacked{}
			}
			cfg.keychainFields.mark(ctxName, field)
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
	for _, ctx := range cfg.Contexts {
		if ctx == nil {
			continue
		}
		for _, field := range credentials.AllFields {
			if ref, ok := fieldRef(ctx, field); ok && ref.get() != "" {
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
//     writing it back verbatim, never touching the store;
//   - deletes the keychain entry for a field that was backed but is now empty
//     (gcx config unset, or an auth-method switch that drops the credential);
//   - writes any plaintext secret through to the keychain and substitutes a
//     sentinel for the on-disk value, covering both migration and freshly
//     written secrets from gcx login / gcx config set.
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
	for ctxName, ctx := range cfg.Contexts {
		if ctx == nil {
			continue
		}
		for _, field := range credentials.AllFields {
			ref, ok := fieldRef(ctx, field)
			if !ok {
				continue
			}
			key := credentials.AccountKey(ctxName, field)

			// Unresolvable at load: round-trip the sentinel verbatim.
			if cfg.keychainPreserve[ctxName][field] {
				swaps = append(swaps, swap{ref: ref, plaintext: ref.get()})
				ref.set(credentials.FormatSentinel(ctxName, field))
				continue
			}

			cur := ref.get()

			if cur == "" {
				// Field cleared. If it was keychain-backed, remove the now-stale
				// entry instead of orphaning it.
				if cfg.keychainFields[ctxName][field] {
					if err := store.Delete(key); err != nil && !errors.Is(err, credentials.ErrUnavailable) {
						log.Warn("could not remove stale keychain entry",
							"context", ctxName,
							"field", string(field),
							"error", err.Error())
					}
					delete(cfg.keychainFields[ctxName], field)
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
					"context", ctxName,
					"field", string(field),
					"error", err.Error())
				continue
			}
			cfg.keychainFields.mark(ctxName, field)
			swaps = append(swaps, swap{ref: ref, plaintext: cur})
			ref.set(credentials.FormatSentinel(ctxName, field))
		}
	}
	return func() {
		for _, s := range swaps {
			s.ref.set(s.plaintext)
		}
	}
}
