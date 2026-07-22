// Package auth provides shared authentication helpers for the adaptive telemetry provider.
// It is a separate package to avoid import cycles between the parent adaptive package
// (which imports signal subpackages) and the signal subpackages (which need auth helpers).
package auth

import (
	"context"
	"fmt"
	"net/http"
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

// ResolveSignalAuth resolves auth credentials for the given signal ("metrics", "logs", or "traces").
// GCOM stack metadata is authoritative for the tenant URL and ID. Historical
// provider cache fields are never trusted as credential destinations: they can
// become stale when a context changes Cloud entry or stack slug. A runtime URL
// override remains supported only when LoadDirectProviderSnapshot has paired it
// with a GRAFANA_CLOUD_TOKEN supplied in the same invocation.
func ResolveSignalAuth(ctx context.Context, loader *providers.ConfigLoader, signal string) (SignalAuth, error) {
	endpointKey := signal + "-tenant-url"
	snapshot, err := loader.LoadDirectProviderSnapshot(ctx, providers.DirectProviderPolicy{
		ProviderName:    "adaptive",
		EndpointKeys:    []string{endpointKey},
		CredentialEnv:   "GRAFANA_CLOUD_TOKEN",
		RejectAutoLocal: true,
	})
	if err != nil {
		return SignalAuth{}, err
	}

	cloudCfg, err := snapshot.ResolveCloudConfig(ctx)
	if err != nil {
		return SignalAuth{}, fmt.Errorf("adaptive-%s: failed to load cloud config: %w", signal, err)
	}

	baseURL, tenantID, err := ExtractSignalInfo(cloudCfg.Stack, signal)
	if err != nil {
		return SignalAuth{}, err
	}
	if snapshot.EndpointOverriddenByEnvironment(endpointKey) {
		baseURL = snapshot.ProviderConfig[endpointKey]
	}

	httpClient, err := cloudCfg.HTTPClient(ctx)
	if err != nil {
		return SignalAuth{}, fmt.Errorf("adaptive-%s: failed to create HTTP client: %w", signal, err)
	}

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
