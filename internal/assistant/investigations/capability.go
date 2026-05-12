package investigations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/providers"
)

const (
	// capabilityProviderName / capabilityCacheKey identify the per-context slot
	// in gcx config where the Lodestone capability is persisted. Mirrors the
	// SaveProviderConfig precedent used by adaptive, faro, and synth.
	capabilityProviderName = "assistant"
	capabilityCacheKey     = "lodestone-v2"
	envAPIVersionOverride  = "GCX_ASSISTANT_API_VERSION"
)

// Capability reports which Assistant API versions a stack supports.
type Capability struct {
	V2 bool
}

// capabilityProbe runs the network call that distinguishes v2 stacks from v1.
type capabilityProbe func(ctx context.Context, base *assistanthttp.Client) (bool, error)

// DetectCapability returns the v2 capability for the current context. Results
// are cached at contexts.<current>.providers.assistant.lodestone-v2 via
// SaveProviderConfig and reused on subsequent invocations.
//
// Set GCX_ASSISTANT_API_VERSION to v1 or v2 to short-circuit the probe; this
// is intended for tests and not exposed in CLI help.
func DetectCapability(ctx context.Context, loader *providers.ConfigLoader, base *assistanthttp.Client) (Capability, error) {
	return detectCapabilityWith(ctx, loader, base, probeLodestone)
}

func detectCapabilityWith(ctx context.Context, loader *providers.ConfigLoader, base *assistanthttp.Client, probe capabilityProbe) (Capability, error) {
	if v, ok := apiVersionFromEnv(); ok {
		return Capability{V2: v == "v2"}, nil
	}

	if v, ok := loadCachedV2(ctx, loader); ok {
		return Capability{V2: v}, nil
	}

	v2, err := probe(ctx, base)
	if err != nil {
		return Capability{}, err
	}

	// Best-effort persist; mirrors the synth/faro/adaptive precedent.
	_ = loader.SaveProviderConfig(ctx, capabilityProviderName, capabilityCacheKey, strconv.FormatBool(v2))
	return Capability{V2: v2}, nil
}

// probeLodestone calls GET /investigations/lodestone?limit=1 — the canonical
// v2 list endpoint, kept minimal. 200 means Lodestone is supported; 404 means
// v1-only. The profiles endpoint would be cheaper but isn't deployed on every
// stack version, so it's unreliable as a capability signal.
func probeLodestone(ctx context.Context, base *assistanthttp.Client) (bool, error) {
	resp, err := base.DoRequest(ctx, http.MethodGet, lodestoneListPath+"?limit=1", nil)
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

// loadCachedV2 reads providers.assistant.lodestone-v2 from the current context.
// Returns (value, true) on a hit; (false, false) on miss or parse failure so
// callers fall through to a fresh probe.
func loadCachedV2(ctx context.Context, loader *providers.ConfigLoader) (bool, bool) {
	cfg, _, err := loader.LoadProviderConfig(ctx, capabilityProviderName)
	if err != nil {
		return false, false
	}
	raw, ok := cfg[capabilityCacheKey]
	if !ok {
		return false, false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return v, true
}

// errV2NotSupported is returned by v2-only commands when the connected stack
// does not advertise Lodestone. Package-private — used by `requireV2` only;
// CLI callers see it wrapped with the host as a formatted error message.
var errV2NotSupported = errors.New("lodestone (v2 investigations) is not available")

// CachedV2 reports whether v2 is known to be supported for the current context
// based on cached config. Returns false when there is no cached entry; never
// probes the network. Honours GCX_ASSISTANT_API_VERSION.
func CachedV2(ctx context.Context, loader *providers.ConfigLoader) bool {
	if v, ok := apiVersionFromEnv(); ok {
		return v == "v2"
	}
	v, ok := loadCachedV2(ctx, loader)
	return ok && v
}
