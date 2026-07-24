package k6

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
)

// Provider config keys used for cross-invocation auth caching in DirectClient mode.
// These are persisted to the gcx config file so subsequent invocations can skip
// the /v3/account/grafana-app/start round-trip. The cache is bound to its stack,
// API domain, and a commit-last tuple digest; any mismatch invalidates it.
const (
	keyCachedToken   = "cached-token"
	keyCachedOrgID   = "cached-org-id"
	keyCachedStackID = "cached-stack-id"
	keyCachedDomain  = "cached-api-domain"
	keyCachedBinding = "cached-binding"
)

// cacheStore is the persistence sink for the SA-token cache. It is satisfied
// by providers.ConfigLoader; declared locally to keep the cache code
// decoupled from the heavier CloudConfigLoader interface.
type cacheStore interface {
	SaveProviderConfig(ctx context.Context, providerName, key, value string) error
}

// loadCache returns the cached k6 credentials only when its stack, API domain,
// and commit digest match. A legacy, partial, interleaved, or destination-moved
// tuple is a miss; callers fall back to a fresh exchange.
func loadCache(providerCfg map[string]string, currentStackID int, currentDomain string) (string, int, bool) {
	if providerCfg == nil {
		return "", 0, false
	}
	tok := providerCfg[keyCachedToken]
	cachedStack, errStack := strconv.Atoi(providerCfg[keyCachedStackID])
	cachedOrg, errOrg := strconv.Atoi(providerCfg[keyCachedOrgID])
	cachedDomain := normalizeAPIDomain(providerCfg[keyCachedDomain])
	if tok == "" || errStack != nil || errOrg != nil || cachedStack != currentStackID ||
		providerCfg[keyCachedDomain] == "" || cachedDomain != normalizeAPIDomain(currentDomain) ||
		providerCfg[keyCachedBinding] != cacheBinding(tok, cachedOrg, cachedStack, cachedDomain) {
		return "", 0, false
	}
	return tok, cachedOrg, true
}

// persistCache writes the cached fields back to the config under
// providers.k6.* so subsequent invocations skip the /start round-trip.
// Save failures are non-fatal: the in-memory client still works for this run.
func persistCache(ctx context.Context, store cacheStore, token string, orgID, stackID int, apiDomain string) {
	domain := normalizeAPIDomain(apiDomain)
	saves := []struct{ key, val string }{
		{keyCachedToken, token},
		{keyCachedOrgID, strconv.Itoa(orgID)},
		{keyCachedStackID, strconv.Itoa(stackID)},
		{keyCachedDomain, domain},
		// The binding is the commit marker. Writing it last makes every partial
		// or interleaved tuple fail closed on the next load.
		{keyCachedBinding, cacheBinding(token, orgID, stackID, domain)},
	}
	for _, s := range saves {
		if err := store.SaveProviderConfig(ctx, "k6", s.key, s.val); err != nil {
			slog.DebugContext(ctx, "k6: failed to persist cached auth", "key", s.key, "error", err)
			return
		}
	}
}

// clearCache wipes the persisted cache. Called when a cached token is rejected.
func clearCache(ctx context.Context, store cacheStore) {
	// Invalidate the commit marker first. Even if a later field clear fails, a
	// reader cannot accept the remaining tuple.
	for _, key := range []string{keyCachedBinding, keyCachedToken, keyCachedOrgID, keyCachedStackID, keyCachedDomain} {
		if err := store.SaveProviderConfig(ctx, "k6", key, ""); err != nil {
			slog.DebugContext(ctx, "k6: failed to clear cached auth", "key", key, "error", err)
		}
	}
}

func normalizeAPIDomain(domain string) string {
	if domain == "" {
		domain = DefaultAPIDomain
	}
	return strings.TrimRight(strings.TrimSpace(domain), "/")
}

func cacheBinding(token string, orgID, stackID int, apiDomain string) string {
	hasher := sha256.New()
	for _, value := range []string{token, strconv.Itoa(orgID), strconv.Itoa(stackID), normalizeAPIDomain(apiDomain)} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(value))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
