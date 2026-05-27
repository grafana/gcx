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

// fieldAddr returns a pointer to the in-struct secret field for the given
// context and field key, or nil if the field's parent struct is not present.
func fieldAddr(ctx *Context, field credentials.Field) *string {
	if field == credentials.FieldCloudToken {
		if ctx.Cloud == nil {
			return nil
		}
		return &ctx.Cloud.Token
	}
	if ctx.Grafana == nil {
		return nil
	}
	switch field {
	case credentials.FieldGrafanaToken:
		return &ctx.Grafana.APIToken
	case credentials.FieldGrafanaPassword:
		return &ctx.Grafana.Password
	case credentials.FieldOAuthToken:
		return &ctx.Grafana.OAuthToken
	case credentials.FieldOAuthRefreshToken:
		return &ctx.Grafana.OAuthRefreshToken
	}
	return nil
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
			ptr := fieldAddr(ctx, field)
			if ptr == nil || !credentials.IsSentinel(*ptr) {
				continue
			}
			parsedCtx, parsedField, ok := credentials.ParseSentinel(*ptr)
			if !ok || parsedCtx != ctxName || parsedField != field {
				log.Warn("ignoring malformed keychain sentinel",
					"context", ctxName,
					"field", string(field),
					"value", *ptr)
				*ptr = ""
				continue
			}
			value, err := store.Get(credentials.AccountKey(ctxName, field))
			if err != nil {
				log.Warn("could not resolve keychain entry",
					"context", ctxName,
					"field", string(field),
					"error", err.Error())
				*ptr = ""
				continue
			}
			*ptr = value
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
			ptr := fieldAddr(ctx, field)
			if ptr == nil || *ptr == "" || credentials.IsSentinel(*ptr) {
				continue
			}
			if cfg.keychainFields[ctxName][field] {
				continue
			}
			if err := store.Set(credentials.AccountKey(ctxName, field), *ptr); err != nil {
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
		ptr       *string
		plaintext string
	}
	var swaps []swap
	for ctxName, fields := range cfg.keychainFields {
		ctx := cfg.Contexts[ctxName]
		if ctx == nil {
			continue
		}
		for field := range fields {
			ptr := fieldAddr(ctx, field)
			if ptr == nil {
				continue
			}
			plaintext := *ptr
			if plaintext != "" && !credentials.IsSentinel(plaintext) {
				if err := store.Set(credentials.AccountKey(ctxName, field), plaintext); err != nil {
					log.Warn("could not update keychain entry",
						"context", ctxName,
						"field", string(field),
						"error", err.Error())
					continue
				}
			}
			swaps = append(swaps, swap{ptr: ptr, plaintext: plaintext})
			*ptr = credentials.FormatSentinel(ctxName, field)
		}
	}
	return func() {
		for _, s := range swaps {
			*s.ptr = s.plaintext
		}
	}
}
