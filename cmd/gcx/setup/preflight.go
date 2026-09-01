package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
)

const (
	// collectorAppID is the Grafana app plugin that proxies Fleet Management.
	collectorAppID = "grafana-collector-app"

	pluginSettingsPath = "/api/plugins/" + collectorAppID + "/settings"
	userActionsPath    = "/api/access-control/user/actions"

	// actionRead is the action a read command needs. actionAdmin is the action
	// every write command needs, because a write matches the wildcard route of
	// the plugin.
	actionRead  = collectorAppID + ":read"
	actionAdmin = collectorAppID + ":admin"

	// maxPreflightBody caps the response bodies the preflight reads.
	maxPreflightBody = 1 << 20
)

// collectorAppState is the result of the Fleet Management preflight: whether
// the plugin serves the proxy routes, and which actions the login holds.
type collectorAppState struct {
	// PluginKnown is false when Grafana answers the plugin settings endpoint
	// with a status other than 200 or 404. A 403 or a 500 says nothing about
	// the plugin, so the preflight must not report the plugin as absent.
	PluginKnown bool
	// PluginStatus is the HTTP status that left the plugin state unknown.
	PluginStatus int
	Installed    bool
	Enabled      bool
	// ActionsKnown is false when Grafana does not answer the actions endpoint.
	ActionsKnown bool
	CanRead      bool
	CanAdmin     bool
}

// checkCollectorApp reads the plugin state and the actions of the current
// login. It returns an error only when the requests themselves fail.
func checkCollectorApp(ctx context.Context, cfg config.NamespacedRESTConfig, httpClient *http.Client) (collectorAppState, error) {
	var state collectorAppState

	var settings struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	status, err := getJSON(ctx, httpClient, cfg.Host+pluginSettingsPath, &settings)
	if err != nil {
		return state, err
	}
	switch status {
	case http.StatusOK:
		state.PluginKnown = true
		state.Installed = true
		state.Enabled = settings.Enabled
	case http.StatusNotFound:
		// Only a 404 means that the plugin is absent.
		state.PluginKnown = true
	default:
		state.PluginStatus = status
	}

	actions := map[string]bool{}
	status, err = getJSON(ctx, httpClient, cfg.Host+userActionsPath, &actions)
	if err != nil {
		return state, err
	}
	if status == http.StatusOK {
		state.ActionsKnown = true
		state.CanRead = actions[actionRead]
		state.CanAdmin = actions[actionAdmin]
	}

	return state, nil
}

// getJSON performs a GET and decodes a 2xx body into out. A non-2xx response
// returns its status code without an error. The caller decides which status
// means "absent" and which status means "unknown".
func getJSON(ctx context.Context, httpClient *http.Client, url string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("preflight: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("preflight: request %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPreflightBody))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("preflight: read %s: %w", url, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, fmt.Errorf("preflight: decode %s: %w", url, err)
	}
	return resp.StatusCode, nil
}

// mayServe reports whether a Fleet Management call through the plugin proxy is
// worth an attempt. An unknown plugin state counts as worth an attempt, so the
// real error of the call reaches the user in place of a guess.
func (s collectorAppState) mayServe() bool {
	if !s.PluginKnown {
		return true
	}
	return s.Installed && s.Enabled
}

// row renders the preflight as a product row of the status document.
func (s collectorAppState) row() setupProductStatus {
	switch {
	case !s.PluginKnown:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: false,
			Health:  "unknown",
			Details: fmt.Sprintf("HTTP %d from %s; the plugin state is unknown", s.PluginStatus, pluginSettingsPath),
		}
	case !s.Installed:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: false,
			Health:  "unhealthy",
			Details: "the " + collectorAppID + " plugin is not installed",
		}
	case !s.Enabled:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: false,
			Health:  "unhealthy",
			Details: "the " + collectorAppID + " plugin is installed but not enabled",
		}
	case s.ActionsKnown && !s.CanRead:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "unhealthy",
			Details: "your login is missing the " + actionRead + " action",
		}
	case s.ActionsKnown && !s.CanAdmin:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "degraded",
			Details: "read only; write commands need the " + actionAdmin + " action",
		}
	case !s.ActionsKnown:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "healthy",
			Details: "plugin enabled; permissions unknown",
		}
	default:
		return setupProductStatus{
			Product: "fleet-management",
			Enabled: true,
			Health:  "healthy",
			Details: "read and write",
		}
	}
}
