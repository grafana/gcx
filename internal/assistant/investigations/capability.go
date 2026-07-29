package investigations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/providers"
)

const (
	// capabilityProviderName / capabilityCacheKey identify the per-context slot
	// in gcx config where the Assistant investigations API mode is persisted.
	// Mirrors the SaveProviderConfig precedent used by adaptive, faro, and synth.
	capabilityProviderName = "assistant"
	capabilityCacheKey     = "api-mode"
	envAPIVersionOverride  = "GCX_ASSISTANT_API_VERSION"
)

// APIMode names which Assistant investigations API surface a stack exposes.
// Values are written verbatim to the gcx config cache, so they must remain
// stable strings.
type APIMode string

const (
	// APIModeLegacy is the original /api/v1 surface (no /api/v2).
	APIModeLegacy APIMode = "v1"
	// APIModeV2Standard is the /api/v2/investigations/* surface introduced by
	// grafana-assistant-app#6645. Preferred when available.
	APIModeV2Standard APIMode = "v2"
)

// SupportsV2 reports whether the mode exposes the v2 investigations feature
// set (pause/resume/mode/share/snapshot/...).
func (m APIMode) SupportsV2() bool {
	return m == APIModeV2Standard
}

// capabilityProbe runs the network calls that pick a mode. Split out for tests.
type capabilityProbe func(ctx context.Context, base *assistanthttp.Client) (APIMode, error)

// DetectAPIMode returns the API mode for the current context. Results are
// cached at stacks.<selected-stack>.providers.assistant.api-mode via
// SaveProviderConfig and reused on subsequent invocations.
//
// Set GCX_ASSISTANT_API_VERSION to v1 or v2 to short-circuit the probe;
// intended for tests and not exposed in CLI help.
func DetectAPIMode(ctx context.Context, loader *providers.ConfigLoader, base *assistanthttp.Client) (APIMode, error) {
	return detectAPIModeWith(ctx, loader, base, probeAPIMode)
}

func detectAPIModeWith(ctx context.Context, loader *providers.ConfigLoader, base *assistanthttp.Client, probe capabilityProbe) (APIMode, error) {
	if m, ok := apiModeFromEnv(); ok {
		return m, nil
	}

	if m, ok := loadCachedMode(ctx, loader); ok {
		return m, nil
	}

	mode, err := probe(ctx, base)
	if err != nil {
		return "", err
	}

	// Only v2 is sticky: stacks are never downgraded, so a detected v2 surface
	// stays v2. v1 is deliberately not persisted — the Lodestone rollout
	// upgrades v1 stacks in place, and a cached "v1" would otherwise strand
	// the user on legacy endpoints forever, re-probing on every invocation.
	if mode == APIModeV2Standard {
		// Best-effort persist; mirrors the synth/faro/adaptive precedent.
		_ = loader.SaveProviderConfig(ctx, capabilityProviderName, capabilityCacheKey, string(mode))
	}
	return mode, nil
}

// probeAPIMode prefers /api/v2; falls back to legacy v1. A minimal list call
// with limit=1 keeps the probe cheap.
func probeAPIMode(ctx context.Context, base *assistanthttp.Client) (APIMode, error) {
	if ok, err := probeOK(ctx, base, v2ListPath+"?limit=1"); err != nil {
		return "", fmt.Errorf("probe v2 investigations: %w", err)
	} else if ok {
		return APIModeV2Standard, nil
	}
	return APIModeLegacy, nil
}

// probeOK returns (true, nil) on HTTP 200, (false, nil) on HTTP 404, and a
// wrapped error for any other status or transport failure.
func probeOK(ctx context.Context, base *assistanthttp.Client, path string) (bool, error) {
	resp, err := base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
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

func apiModeFromEnv() (APIMode, bool) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envAPIVersionOverride)))
	switch v {
	case "v1":
		return APIModeLegacy, true
	case "v2":
		return APIModeV2Standard, true
	}
	return "", false
}

// loadCachedMode reads providers.assistant.api-mode from the current context.
// Returns (APIModeV2Standard, true) only when v2 was previously detected; any
// other value (missing, "v1", or a stale codename) returns ("", false) so the
// caller re-probes. This keeps v2 sticky while letting a v1 stack that later
// gains Lodestone be re-detected.
func loadCachedMode(ctx context.Context, loader *providers.ConfigLoader) (APIMode, bool) {
	cfg, _, err := loader.LoadProviderConfig(ctx, capabilityProviderName)
	if err != nil {
		return "", false
	}
	raw, ok := cfg[capabilityCacheKey]
	if !ok {
		return "", false
	}
	// Only v2 is treated as a cache hit. A persisted "v1" (written by older gcx
	// versions) or any stale value falls through to a fresh probe so a stack
	// upgraded to Lodestone is picked up. v2 is never downgraded, so it sticks.
	if APIMode(raw) == APIModeV2Standard {
		return APIModeV2Standard, true
	}
	return "", false
}

// errV2NotSupported is returned by v2-only commands when the connected stack
// does not advertise /api/v2. Package-private — used by requireV2 only; CLI
// callers see it wrapped with the host as a formatted error message.
var errV2NotSupported = errors.New("the v2 investigations API is not available")

// CachedAPIMode reports the API mode persisted for the current context.
// Returns APIModeLegacy (the zero functional value) when there is no cached
// entry; never probes the network. Honours GCX_ASSISTANT_API_VERSION.
func CachedAPIMode(ctx context.Context, loader *providers.ConfigLoader) APIMode {
	if m, ok := apiModeFromEnv(); ok {
		return m
	}
	if m, ok := loadCachedMode(ctx, loader); ok {
		return m
	}
	return APIModeLegacy
}
