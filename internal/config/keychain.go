package config

import (
	"errors"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/grafana-app-sdk/logging"
)

// keychainBacked tracks which (context, field) pairs were stored in the
// keychain at load time. The map lives on Config as an unexported field; it is
// populated by resolveSentinels during Load and consumed by substituteSentinels
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
// their plaintext values from the store. The map of resolved (context, field)
// pairs is returned so the writer can re-substitute sentinels on Write. Fields
// whose store lookup fails are cleared to an empty string and logged; the
// command will surface this as an auth failure rather than silently sending a
// sentinel as a credential.
func resolveSentinels(cfg *Config, store credentials.Store, log logging.Logger) keychainBacked {
	backed := keychainBacked{}
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
				log.Warn("could not resolve keychain entry",
					"context", ctxName,
					"field", string(field),
					"error", err.Error())
				ref.set("")
				continue
			}
			ref.set(value)
			backed.mark(ctxName, field)
		}
	}
	return backed
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

// substituteSentinels temporarily replaces keychain-backed fields with their
// sentinel forms (after pushing the current plaintext value to the store) and
// returns a restore function that reverts the in-memory values. Intended to
// wrap a single YAML encode operation.
func substituteSentinels(cfg *Config, store credentials.Store, log logging.Logger) func() {
	type swap struct {
		ref       secretRef
		plaintext string
	}
	var swaps []swap
	for ctxName, fields := range cfg.keychainFields {
		ctx := cfg.Contexts[ctxName]
		if ctx == nil {
			continue
		}
		for field := range fields {
			ref, ok := fieldRef(ctx, field)
			if !ok {
				continue
			}
			plaintext := ref.get()
			if plaintext != "" && !credentials.IsSentinel(plaintext) {
				if err := store.Set(credentials.AccountKey(ctxName, field), plaintext); err != nil {
					log.Warn("could not update keychain entry",
						"context", ctxName,
						"field", string(field),
						"error", err.Error())
					continue
				}
			}
			swaps = append(swaps, swap{ref: ref, plaintext: plaintext})
			ref.set(credentials.FormatSentinel(ctxName, field))
		}
	}
	return func() {
		for _, s := range swaps {
			s.ref.set(s.plaintext)
		}
	}
}
