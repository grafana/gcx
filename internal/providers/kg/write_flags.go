package kg

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	reDomain     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// parseEntityRefToken parses a compact "domain/Type/name" ref token. Scope is
// supplied separately via --from-scope/--to-scope, so it is not part of the token.
func parseEntityRefToken(token string) (EntityRef, error) {
	parts := strings.Split(token, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return EntityRef{}, fmt.Errorf("invalid ref %q: expected domain/Type/name (e.g. myapp/Service/checkout)", token)
	}
	return EntityRef{Domain: parts[0], Type: parts[1], Name: parts[2]}, nil
}

// parseTTL converts a duration string to seconds for the API's ttlSeconds field.
// "" => -1 (never expire); negative durations stay negative (also never expire);
// "0" => 0 (expire immediately); the "Nd" day suffix is supported.
func parseTTL(s string) (int64, error) {
	if s == "" {
		return -1, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return int64(d.Seconds()), nil
	}
	// Not a Go duration — try the "Nd" day suffix. ParseInt (not Sscanf) so
	// trailing junk errors instead of being silently truncated (e.g. "1.5d").
	days, ok := strings.CutSuffix(s, "d")
	if !ok {
		return 0, fmt.Errorf("invalid --ttl %q: use a duration like 1h, 30m, 7d", s)
	}
	n, err := strconv.ParseInt(days, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid --ttl %q: use a duration like 1h, 30m, 7d", s)
	}
	const secondsPerDay = 86400
	if n > math.MaxInt64/secondsPerDay || n < math.MinInt64/secondsPerDay {
		return 0, fmt.Errorf("invalid --ttl %q: day value out of range", s)
	}
	return n * secondsPerDay, nil
}

func validateIdentifier(s, field string) error {
	if !reIdentifier.MatchString(s) {
		return fmt.Errorf("%s %q must be a valid identifier (letter followed by letters/digits/underscores)", field, s)
	}
	return nil
}

func validateDomain(d string) error {
	if !reDomain.MatchString(d) {
		return fmt.Errorf("domain %q must be a lowercase k8s-style slug", d)
	}
	return nil
}

func validateWritableDomain(d string) error {
	if d == "kg" {
		return fmt.Errorf("domain %q is reserved (telemetry); choose a different writable domain", d)
	}
	return validateDomain(d)
}

// validateKgKeys mirrors the backend @SafeKgKeys constraint: every scope/property
// key must be a valid identifier (which also rejects empty and '_'-prefixed keys)
// and must not be a reserved identity field ("name"/"type"). field names the flag
// for the error message (e.g. "scope", "property").
func validateKgKeys(m map[string]string, field string) error {
	for k := range m {
		if k == "name" || k == "type" {
			return fmt.Errorf("%s key %q is reserved (identity field); choose a different key", field, k)
		}
		if !reIdentifier.MatchString(k) {
			return fmt.Errorf("%s key %q must be a valid identifier (letter followed by letters/digits/underscores; no leading underscore)", field, k)
		}
	}
	return nil
}

// validateNoScopePropertyOverlap mirrors the backend @PropertiesNotInScope
// constraint: a property key may not shadow a scope (identity) key.
func validateNoScopePropertyOverlap(scope, properties map[string]string) error {
	for k := range properties {
		if _, ok := scope[k]; ok {
			return fmt.Errorf("key %q appears in both scope and property; properties may not shadow scope (identity) keys", k)
		}
	}
	return nil
}
