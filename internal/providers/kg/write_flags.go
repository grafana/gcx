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
	// reTenantDomain matches the domain of a schema the stack publishes itself
	// (a tenant KgSchema), which the KG API addresses as tenant/<name>. The name
	// grammar is narrower than a slug and mirrors the API's: a lowercase letter,
	// then lowercase letters and digits.
	reTenantDomain = regexp.MustCompile(`^tenant/[a-z][a-z0-9]*$`)
)

// tenantDomainPrefix starts every tenant domain.
const tenantDomainPrefix = "tenant/"

// parseEntityRefToken parses a compact "domain/Type/name" ref token. A tenant
// domain contains a slash of its own, so its token is tenant/<name>/Type/name.
// No other domain and no type contains "/", so the name is everything after
// the type and may itself contain "/" (for example github.com/contoso/shop).
// Scope is supplied separately via --from-scope/--to-scope, so it is not part
// of the token.
func parseEntityRefToken(token string) (EntityRef, error) {
	parts := strings.SplitN(token, "/", 4)
	if len(parts) == 4 && parts[0] == "tenant" && parts[1] != "" {
		parts = []string{tenantDomainPrefix + parts[1], parts[2], parts[3]}
	} else {
		parts = strings.SplitN(token, "/", 3)
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return EntityRef{}, fmt.Errorf("invalid ref %q: expected domain/Type/name (e.g. myapp/Service/checkout, or tenant/myapp/Service/checkout for a tenant domain)", token)
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

// validateDomain accepts a lowercase k8s-style slug, or a tenant domain
// (tenant/<name>) for a schema the stack publishes itself.
func validateDomain(d string) error {
	if reDomain.MatchString(d) || reTenantDomain.MatchString(d) {
		return nil
	}
	if strings.HasPrefix(d, tenantDomainPrefix) {
		return fmt.Errorf("tenant domain %q must be tenant/ followed by a lowercase letter and then lowercase letters or digits", d)
	}
	return fmt.Errorf("domain %q must be a lowercase k8s-style slug", d)
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
