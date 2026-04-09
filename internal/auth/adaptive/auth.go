// Package auth provides shared authentication helpers for the adaptive telemetry provider.
// It is a separate package to avoid import cycles between the parent adaptive package
// (which imports signal subpackages) and the signal subpackages (which need auth helpers).
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
)

// SignalAuth holds resolved auth credentials for a single adaptive telemetry signal.
type SignalAuth struct {
	BaseURL    string
	TenantID   int
	APIToken   string
	HTTPClient *http.Client
}

// getCachedSignalAuth attempts to retrieve cached signal auth from provider config.
// Returns (SignalAuth, true, nil) if cached values are found and valid,
// returns (empty, false, nil) on cache miss, or (empty, false, error) on actual errors.
func getCachedSignalAuth(ctx context.Context, loader *providers.ConfigLoader, signal string) (SignalAuth, bool, error) {
	provCfg, _, err := loader.LoadProviderConfig(ctx, "adaptive")
	if err != nil {
		// If cache is completely unavailable, treat as cache miss (proceed to GCOM).
		return SignalAuth{}, false, nil //nolint:nilerr // Cache miss is not an error.
	}
	if provCfg == nil {
		return SignalAuth{}, false, nil
	}

	cachedURL := provCfg[signal+"-tenant-url"]
	cachedID := provCfg[signal+"-tenant-id"]
	if cachedURL == "" || cachedID == "" {
		return SignalAuth{}, false, nil
	}

	tenantID, parseErr := strconv.Atoi(cachedID)
	if parseErr != nil {
		// Corrupted cache entry; treat as cache miss.
		return SignalAuth{}, false, nil //nolint:nilerr // Corrupted cache is a cache miss, not an error.
	}

	cloudCfg, cloudErr := loader.LoadCloudConfig(ctx)
	if cloudErr != nil {
		return SignalAuth{}, false, fmt.Errorf("adaptive: failed to load cloud config for token: %w", cloudErr)
	}

	httpClient, httpErr := cloudCfg.HTTPClient(ctx)
	if httpErr != nil {
		return SignalAuth{}, false, fmt.Errorf("adaptive: failed to create HTTP client: %w", httpErr)
	}

	return SignalAuth{
		BaseURL:    strings.TrimRight(cachedURL, "/"),
		TenantID:   tenantID,
		APIToken:   cloudCfg.Token,
		HTTPClient: httpClient,
	}, true, nil
}

// ResolveSignalAuth resolves auth credentials for the given signal ("metrics", "logs", or "traces").
// It checks cached provider config first, falling back to a GCOM lookup via LoadCloudConfig.
// On GCOM lookup, it caches the resolved values for subsequent calls.
func ResolveSignalAuth(ctx context.Context, loader *providers.ConfigLoader, signal string) (SignalAuth, error) {
	// Check cached values first.
	cached, found, err := getCachedSignalAuth(ctx, loader, signal)
	if err != nil {
		return SignalAuth{}, err
	}
	if found {
		return cached, nil
	}

	// Cache miss — resolve via GCOM.
	cloudCfg, err := loader.LoadCloudConfig(ctx)
	if err != nil {
		return SignalAuth{}, fmt.Errorf("adaptive: failed to load cloud config: %w", err)
	}

	baseURL, tenantID, err := ExtractSignalInfo(cloudCfg.Stack, signal)
	if err != nil {
		return SignalAuth{}, err
	}

	httpClient, err := cloudCfg.HTTPClient(ctx)
	if err != nil {
		return SignalAuth{}, fmt.Errorf("adaptive: failed to create HTTP client: %w", err)
	}

	// Cache resolved values for subsequent calls.
	_ = loader.SaveProviderConfig(ctx, "adaptive", signal+"-tenant-id", strconv.Itoa(tenantID))
	_ = loader.SaveProviderConfig(ctx, "adaptive", signal+"-tenant-url", baseURL)

	return SignalAuth{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		TenantID:   tenantID,
		APIToken:   cloudCfg.Token,
		HTTPClient: httpClient,
	}, nil
}

// ExtractSignalInfo maps a signal name to the corresponding StackInfo fields.
func ExtractSignalInfo(stack cloud.StackInfo, signal string) (string, int, error) {
	var baseURL string
	var tenantID int
	switch signal {
	case "metrics":
		baseURL = stack.HMInstancePromURL
		tenantID = stack.HMInstancePromID
	case "logs":
		baseURL = stack.HLInstanceURL
		tenantID = stack.HLInstanceID
	case "traces":
		baseURL = stack.HTInstanceURL
		tenantID = stack.HTInstanceID
	default:
		return "", 0, fmt.Errorf("adaptive: unknown signal %q", signal)
	}

	if baseURL == "" {
		return "", 0, fmt.Errorf("adaptive %s: instance URL is not available for this stack", signal)
	}
	if tenantID == 0 {
		return "", 0, fmt.Errorf("adaptive %s: instance ID is not available for this stack", signal)
	}

	return baseURL, tenantID, nil
}
