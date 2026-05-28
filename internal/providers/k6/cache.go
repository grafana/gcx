package k6

import (
	"context"
	"log/slog"
	"strconv"
)

// Provider config keys used for cross-invocation auth caching in DirectClient mode.
// These are persisted to the gcx config file so subsequent invocations can skip
// the /v3/account/grafana-app/start round-trip. The cache is keyed by stack ID;
// any mismatch invalidates the entry.
const (
	keyCachedToken   = "cached-token"
	keyCachedOrgID   = "cached-org-id"
	keyCachedStackID = "cached-stack-id"
)

// cacheStore is the persistence sink for the SA-token cache. It is satisfied
// by providers.ConfigLoader; declared locally to keep the cache code
// decoupled from the heavier CloudConfigLoader interface.
type cacheStore interface {
	SaveProviderConfig(ctx context.Context, providerName, key, value string) error
}

// loadCache returns the cached k6 credentials if the cache is bound to the
// current stack ID. A mismatch (stack changed) is treated as a miss; callers
// should fall back to a fresh exchange, which will overwrite the stale entry.
func loadCache(providerCfg map[string]string, currentStackID int) (string, int, bool) {
	if providerCfg == nil {
		return "", 0, false
	}
	tok := providerCfg[keyCachedToken]
	cachedStack, errStack := strconv.Atoi(providerCfg[keyCachedStackID])
	cachedOrg, errOrg := strconv.Atoi(providerCfg[keyCachedOrgID])
	if tok == "" || errStack != nil || errOrg != nil || cachedStack != currentStackID {
		return "", 0, false
	}
	return tok, cachedOrg, true
}

// persistCache writes the cached fields back to the config under
// providers.k6.* so subsequent invocations skip the /start round-trip.
// Save failures are non-fatal: the in-memory client still works for this run.
func persistCache(ctx context.Context, store cacheStore, token string, orgID, stackID int) {
	saves := []struct{ key, val string }{
		{keyCachedToken, token},
		{keyCachedOrgID, strconv.Itoa(orgID)},
		{keyCachedStackID, strconv.Itoa(stackID)},
	}
	for _, s := range saves {
		if err := store.SaveProviderConfig(ctx, "k6", s.key, s.val); err != nil {
			slog.DebugContext(ctx, "k6: failed to persist cached auth", "key", s.key, "error", err)
		}
	}
}

// clearCache wipes the persisted cache. Called when a cached token is rejected.
func clearCache(ctx context.Context, store cacheStore) {
	for _, key := range []string{keyCachedToken, keyCachedOrgID, keyCachedStackID} {
		if err := store.SaveProviderConfig(ctx, "k6", key, ""); err != nil {
			slog.DebugContext(ctx, "k6: failed to clear cached auth", "key", key, "error", err)
		}
	}
}
