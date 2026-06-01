// Package credentials moves token-shaped secrets from gcx's YAML config into
// the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret
// Service). When a secret has been moved, the config file holds a sentinel
// string of the form "keychain:gcx:<context>:<field>" in place of the
// plaintext value; the loader resolves these sentinels back to plaintext in
// memory, so existing readers of the config require no changes.
//
// Migration is automatic and idempotent: on every config load, plaintext
// secrets are pushed into the keychain and replaced with sentinels in the
// YAML. If the keychain is unavailable (headless boxes, locked sessions,
// missing DBus), gcx falls back to leaving plaintext in place and emits a
// one-time warning.
package credentials

import (
	"errors"
	"strings"
)

// service is the keychain "service" name used for all gcx entries.
const service = "gcx"

// sentinelPrefix marks a config value as a reference to a keychain entry.
const sentinelPrefix = "keychain:" + service + ":"

// Field identifies one of the token-shaped secret fields stored per context.
// The string values are also the per-context account suffix used in the
// keychain entry name.
type Field string

const (
	FieldCloudToken      Field = "cloud-token"
	FieldGrafanaToken    Field = "grafana-token"
	FieldGrafanaPassword Field = "grafana-password"
	//nolint:gosec // field identifier, not a credential.
	FieldOAuthToken Field = "oauth-token"
	//nolint:gosec // field identifier, not a credential.
	FieldOAuthRefreshToken Field = "oauth-refresh-token"
	FieldSMToken           Field = "sm-token"
)

// AllFields lists every secret field handled by this package.
//
//nolint:gochecknoglobals // constant-like lookup list; never mutated.
var AllFields = []Field{
	FieldCloudToken,
	FieldGrafanaToken,
	FieldGrafanaPassword,
	FieldOAuthToken,
	FieldOAuthRefreshToken,
	FieldSMToken,
}

// ErrNotFound is returned by Store.Get when no entry exists for the given key.
var ErrNotFound = errors.New("credentials: entry not found")

// ErrUnavailable is returned when the OS keychain cannot be reached. Callers
// should fall back to plaintext.
var ErrUnavailable = errors.New("credentials: keychain unavailable")

// Store is the minimal interface for a secret backend.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// AccountKey returns the keychain account string for a context/field pair.
func AccountKey(context string, field Field) string {
	return context + ":" + string(field)
}

// FormatSentinel returns the YAML sentinel value that represents a keychain-
// backed secret for the given context and field.
func FormatSentinel(context string, field Field) string {
	return sentinelPrefix + AccountKey(context, field)
}

// IsSentinel reports whether s is a keychain sentinel.
func IsSentinel(s string) bool {
	return strings.HasPrefix(s, sentinelPrefix)
}

// ParseSentinel extracts the context and field from a sentinel string. The
// third return value is false if s is not a recognised sentinel.
func ParseSentinel(s string) (string, Field, bool) {
	if !IsSentinel(s) {
		return "", "", false
	}
	rest := strings.TrimPrefix(s, sentinelPrefix)
	idx := strings.LastIndex(rest, ":")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], Field(rest[idx+1:]), true
}
