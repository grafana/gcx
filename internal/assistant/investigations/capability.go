package investigations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

const (
	capabilityCacheFileName = "lodestone-capability.json"
	capabilityCacheTTL      = 24 * time.Hour
	envAPIVersionOverride   = "GCX_ASSISTANT_API_VERSION"
)

// Capability reports which Assistant API versions a stack supports.
type Capability struct {
	V2        bool      `json:"v2"`
	CheckedAt time.Time `json:"checkedAt"`
}

// capabilityCache is the on-disk persistence format.
type capabilityCache struct {
	Entries map[string]Capability `json:"entries"`
}

// capabilityProbe runs the network call that distinguishes v2 stacks from v1.
type capabilityProbe func(ctx context.Context, base *assistanthttp.Client) (bool, error)

// CapabilityCachePath returns the on-disk path used to cache probe results.
func CapabilityCachePath() string {
	return filepath.Join(xdg.StateHome, "gcx", capabilityCacheFileName)
}

// detectorMu serializes cache reads/writes so concurrent commands don't race
// on the file.
var detectorMu sync.Mutex //nolint:gochecknoglobals // process-wide cache lock

// DetectCapability returns the v2 capability for the given REST host. Results
// are cached at CapabilityCachePath() for capabilityCacheTTL.
//
// Set GCX_ASSISTANT_API_VERSION to v1 or v2 to short-circuit the probe; this
// is intended for tests and not exposed in CLI help.
func DetectCapability(ctx context.Context, base *assistanthttp.Client, host string) (Capability, error) {
	return detectCapabilityWith(ctx, base, host, probeLodestone, time.Now())
}

func detectCapabilityWith(ctx context.Context, base *assistanthttp.Client, host string, probe capabilityProbe, now time.Time) (Capability, error) {
	if v, ok := apiVersionFromEnv(); ok {
		return Capability{V2: v == "v2", CheckedAt: now}, nil
	}

	detectorMu.Lock()
	defer detectorMu.Unlock()

	path := CapabilityCachePath()
	cache := loadCapabilityCache(path)
	key := cacheKey(host)
	if entry, ok := cache.Entries[key]; ok && now.Sub(entry.CheckedAt) < capabilityCacheTTL {
		return entry, nil
	}

	v2, err := probe(ctx, base)
	if err != nil {
		return Capability{}, err
	}

	entry := Capability{V2: v2, CheckedAt: now}
	if cache.Entries == nil {
		cache.Entries = map[string]Capability{}
	}
	cache.Entries[key] = entry
	_ = saveCapabilityCache(path, cache)
	return entry, nil
}

// probeLodestone calls GET /investigations/lodestone/profiles — the cheapest
// idempotent v2-only endpoint. 200 means v2 is supported; 404 means v1-only.
// Any other transport-level error is returned to the caller.
func probeLodestone(ctx context.Context, base *assistanthttp.Client) (bool, error) {
	resp, err := base.DoRequest(ctx, http.MethodGet, lodestoneProfilesPath, nil)
	if err != nil {
		return false, fmt.Errorf("probe lodestone capability: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, assistanthttp.HandleErrorResponse(resp)
	}
}

func apiVersionFromEnv() (string, bool) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envAPIVersionOverride)))
	if v == "v1" || v == "v2" {
		return v, true
	}
	return "", false
}

func cacheKey(host string) string {
	return strings.TrimRight(host, "/")
}

func loadCapabilityCache(path string) capabilityCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return capabilityCache{Entries: map[string]Capability{}}
	}
	var c capabilityCache
	if err := json.Unmarshal(data, &c); err != nil {
		return capabilityCache{Entries: map[string]Capability{}}
	}
	if c.Entries == nil {
		c.Entries = map[string]Capability{}
	}
	return c
}

func saveCapabilityCache(path string, c capabilityCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ErrV2NotSupported is returned by v2-only commands when the connected stack
// does not advertise Lodestone.
var ErrV2NotSupported = errors.New("lodestone (v2 investigations) is not available on this stack")
