package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
)

// BuildInfo holds the build metadata embedded in /api/frontend/settings.
type BuildInfo struct {
	// GrafanaURL is the canonical URL of the Grafana instance, used by
	// pre-auth target detection to distinguish Cloud from on-prem deployments.
	GrafanaURL string `json:"grafanaUrl"`
}

// FrontendSettings captures the fields exposed at /api/frontend/settings that
// gcx uses for target detection and renderer diagnostics.
type FrontendSettings struct {
	BuildInfo         BuildInfo `json:"buildInfo"`
	RendererAvailable *bool     `json:"rendererAvailable,omitempty"`
}

const frontendSettingsPath = "/api/frontend/settings"

// FetchFrontendSettings performs a GET of /api/frontend/settings
// against baseURL using the supplied client, including its authentication.
// When httpClient is nil it
// falls back to httputils.NewDefaultClient (so --insecure-log-http-payload is honoured).
// Respects the supplied context's deadline and cancellation.
//
// Errors are returned as-is; callers are responsible for deciding how to classify
// them (e.g., mapping a non-200 status to TargetUnknown).
func FetchFrontendSettings(ctx context.Context, baseURL string, httpClient *http.Client) (*FrontendSettings, error) {
	settingsURL := strings.TrimSuffix(baseURL, "/") + frontendSettingsPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, settingsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", settingsURL, err)
	}

	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(ctx)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", settingsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", settingsURL, resp.StatusCode)
	}

	var settings FrontendSettings
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return nil, fmt.Errorf("decoding %s response: %w", frontendSettingsPath, err)
	}

	return &settings, nil
}
