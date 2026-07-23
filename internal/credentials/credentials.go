// Package credentials moves token-shaped secrets from gcx's YAML config into
// the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret
// Service). When a secret has been moved, the config file holds a sentinel
// string containing an opaque binding digest and generation in place of the
// plaintext value; the loader resolves it back to plaintext in memory.
//
// Migration is automatic and idempotent: on every config load, plaintext
// secrets are pushed into the keychain and replaced with sentinels in the
// YAML. If the keychain is unavailable (headless boxes, locked sessions,
// missing DBus), gcx falls back to leaving plaintext in place and emits a
// one-time warning.
package credentials

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// service is the keychain "service" name used for all gcx entries.
const service = "gcx"

// legacySentinelPrefix marks the original, unbound keychain reference format.
// It is retained only so the trusted config migrator can read pre-v2 configs.
// Ordinary config loading must use BoundSentinel and must never resolve this
// format: its owner is chosen entirely by config-file contents.
const legacySentinelPrefix = "keychain:" + service + ":"

// boundSentinelPrefix marks a source/owner/field/destination-bound credential
// reference. The suffix is the same digest used by BoundAccountKey.
const boundSentinelPrefix = legacySentinelPrefix + "v2:"

// boundAccountPrefix separates bound account names from legacy owner:field
// account names in the OS keychain.
const boundAccountPrefix = "v2:"

const staticGeneration = "static"

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

// Binding is the complete authority boundary for one keychain credential.
// Source must be a canonical config-file identity and Destination must be the
// normalized endpoint to which the credential can be presented. Changing any
// component produces a different account and sentinel.
type Binding struct {
	Source      string
	Owner       string
	Field       Field
	Destination string
}

// Valid reports whether every required binding component is present and the
// field is one of the credential fields understood by gcx.
func (b Binding) Valid() bool {
	return b.Source != "" && b.Owner != "" && b.Destination != "" && IsKnownField(b.Field)
}

// IsKnownField reports whether field is part of gcx's credential schema.
func IsKnownField(field Field) bool {
	return slices.Contains(AllFields, field)
}

// BoundReference is one generation-addressed v2 config/keychain pair. A
// credential rotation always creates a fresh generation so the old sentinel
// continues resolving the old value until the config rename commits.
type BoundReference struct {
	Account  string
	Sentinel string
}

// NewBoundReference returns a cryptographically random generation for binding.
func NewBoundReference(binding Binding) (BoundReference, error) {
	if !binding.Valid() {
		return BoundReference{}, errors.New("credentials: invalid credential binding")
	}
	generationBytes := make([]byte, 18)
	if _, err := rand.Read(generationBytes); err != nil {
		return BoundReference{}, fmt.Errorf("credentials: generate credential reference: %w", err)
	}
	return boundReference(binding, base64.RawURLEncoding.EncodeToString(generationBytes)), nil
}

// BoundAccountKey returns a deterministic test/fixture account for binding.
// Production rotations use NewBoundReference.
func BoundAccountKey(binding Binding) string {
	return boundReference(binding, staticGeneration).Account
}

// FormatBoundSentinel returns a deterministic test/fixture sentinel for
// binding. Production rotations use NewBoundReference.
func FormatBoundSentinel(binding Binding) string {
	if !binding.Valid() {
		return ""
	}
	return boundReference(binding, staticGeneration).Sentinel
}

func boundReference(binding Binding, generation string) BoundReference {
	digest := bindingDigest(binding)
	return BoundReference{
		Account:  boundAccountPrefix + digest + ":" + generation,
		Sentinel: boundSentinelPrefix + digest + ":" + generation,
	}
}

// MatchesBoundSentinel reports whether sentinel grants access to exactly the
// supplied binding. It performs no keychain lookup.
func MatchesBoundSentinel(sentinel string, binding Binding) bool {
	digest, _, ok := parseBoundSentinel(sentinel)
	return ok && binding.Valid() && digest == bindingDigest(binding)
}

// AccountForBoundSentinel returns the generation-addressed account selected by
// sentinel only when its binding digest matches binding.
func AccountForBoundSentinel(sentinel string, binding Binding) (string, bool) {
	digest, generation, ok := parseBoundSentinel(sentinel)
	if !ok || !binding.Valid() || digest != bindingDigest(binding) {
		return "", false
	}
	return boundAccountPrefix + digest + ":" + generation, true
}

// MatchesBoundAccount reports whether account belongs to binding, independent
// of its generation.
func MatchesBoundAccount(account string, binding Binding) bool {
	if !binding.Valid() || !strings.HasPrefix(account, boundAccountPrefix) {
		return false
	}
	rest := strings.TrimPrefix(account, boundAccountPrefix)
	digest, generation, ok := strings.Cut(rest, ":")
	return ok && validBindingDigest(digest) && validGeneration(generation) && digest == bindingDigest(binding)
}

// IsBoundSentinel reports whether s uses the v2 bound-reference format.
func IsBoundSentinel(s string) bool {
	_, _, ok := parseBoundSentinel(s)
	return ok
}

func parseBoundSentinel(s string) (string, string, bool) {
	if !strings.HasPrefix(s, boundSentinelPrefix) {
		return "", "", false
	}
	digest, generation, ok := strings.Cut(strings.TrimPrefix(s, boundSentinelPrefix), ":")
	if !ok || !validBindingDigest(digest) || !validGeneration(generation) {
		return "", "", false
	}
	return digest, generation, true
}

func validBindingDigest(digest string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size
}

func validGeneration(generation string) bool {
	return generation != "" && len(generation) <= 128 && !strings.Contains(generation, ":")
}

// IsLegacySentinel reports whether s uses the original unbound format.
func IsLegacySentinel(s string) bool {
	return strings.HasPrefix(s, legacySentinelPrefix) && !strings.HasPrefix(s, boundSentinelPrefix)
}

func bindingDigest(binding Binding) string {
	h := sha256.New()
	for _, component := range []string{
		binding.Source,
		binding.Owner,
		string(binding.Field),
		binding.Destination,
	} {
		_, _ = h.Write([]byte(component))
		_, _ = h.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// AccountKey returns the keychain account string for an owner/field pair.
// Owners are "stack:<name>" or "cloud:<name>" keys (see StackOwner and
// CloudOwner); bare owner strings are legacy per-context keys, kept resolvable
// so configs migrated from the legacy format keep working.
func AccountKey(owner string, field Field) string {
	return owner + ":" + string(field)
}

// StackOwner returns the keychain owner key for a named stack entry.
func StackOwner(name string) string {
	return "stack:" + name
}

// CloudOwner returns the keychain owner key for a named cloud auth entry.
func CloudOwner(name string) string {
	return "cloud:" + name
}

// FormatSentinel returns the legacy, unbound YAML sentinel. New config writes
// must use FormatBoundSentinel. This helper remains for trusted legacy
// migration and rollback compatibility only.
func FormatSentinel(context string, field Field) string {
	return legacySentinelPrefix + AccountKey(context, field)
}

// IsSentinel reports whether s is a keychain sentinel.
func IsSentinel(s string) bool {
	return strings.HasPrefix(s, legacySentinelPrefix)
}

// ParseSentinel extracts the context and field from a sentinel string. The
// third return value is false if s is not a recognised sentinel.
func ParseSentinel(s string) (string, Field, bool) {
	if !IsLegacySentinel(s) {
		return "", "", false
	}
	rest := strings.TrimPrefix(s, legacySentinelPrefix)
	idx := strings.LastIndex(rest, ":")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	field := Field(rest[idx+1:])
	if !IsKnownField(field) {
		return "", "", false
	}
	return rest[:idx], field, true
}
