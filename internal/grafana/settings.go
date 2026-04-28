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
// gcx cares about pre-authentication (target detection).
type FrontendSettings struct {
	BuildInfo BuildInfo `json:"buildInfo"`
}

// FetchAnonymousSettings performs an unauthenticated GET of /api/frontend/settings
// against baseURL, used for pre-auth target detection. When httpClient is nil it
// falls back to httputils.NewDefaultClient (so --log-http-payload is honoured).
// Respects the supplied context's deadline and cancellation.
//
// Errors are returned as-is; callers are responsible for deciding how to classify
// them (e.g., mapping a non-200 status to TargetUnknown).
func FetchAnonymousSettings(ctx context.Context, baseURL string, httpClient *http.Client) (*FrontendSettings, error) {
	settingsURL := strings.TrimSuffix(baseURL, "/") + "/api/frontend/settings"

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
		return nil, fmt.Errorf("decoding /api/frontend/settings response: %w", err)
	}

	return &settings, nil
}
