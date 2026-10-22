package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/httputils"
)

const (
	// CollectorAppSettingsPath returns the state of the collector app plugin.
	CollectorAppSettingsPath = "/api/plugins/" + CollectorAppID + "/settings"

	collectorAppUserActionsPath = "/api/access-control/user/actions"
)

// CollectorAppState describes whether the collector app can serve Fleet
// requests and which route actions the current login has.
type CollectorAppState struct {
	// PluginKnown is false when Grafana returns a status other than 200 or 404.
	PluginKnown bool
	// PluginStatus is the HTTP status that left the plugin state unknown.
	PluginStatus int
	Installed    bool
	Enabled      bool
	// ActionsKnown is false when Grafana does not return the current actions.
	ActionsKnown bool
	CanRead      bool
	CanAdmin     bool
}

// CheckCollectorApp reads the collector app state and the route actions of the
// current login. It checks actions only when the plugin is installed and
// enabled. This keeps a later action request from hiding a known plugin state.
func CheckCollectorApp(ctx context.Context, host string, httpClient *http.Client) (CollectorAppState, error) {
	var state CollectorAppState

	var settings struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	status, err := getJSON(ctx, httpClient, host+CollectorAppSettingsPath, &settings)
	if err != nil {
		return state, err
	}
	switch status {
	case http.StatusOK:
		state.PluginKnown = true
		state.Installed = true
		state.Enabled = settings.Enabled
	case http.StatusNotFound:
		state.PluginKnown = true
	default:
		state.PluginStatus = status
	}

	if !state.PluginKnown || !state.Installed || !state.Enabled {
		return state, nil
	}

	actions := map[string]bool{}
	status, err = getJSON(ctx, httpClient, host+collectorAppUserActionsPath, &actions)
	if err != nil {
		return state, err
	}
	if status == http.StatusOK {
		state.ActionsKnown = true
		state.CanRead = actions[CollectorAppReadAction]
		state.CanAdmin = actions[CollectorAppAdminAction]
	}

	return state, nil
}

// MayServe reports whether a Fleet request through the plugin proxy is useful.
// An unknown plugin state permits a request so that the API can return the
// authoritative error.
func (s CollectorAppState) MayServe() bool {
	if !s.PluginKnown {
		return true
	}
	return s.Installed && s.Enabled
}

// getJSON performs a GET and decodes a successful body into out. For a
// non-success response, it returns the status without an error.
func getJSON(ctx context.Context, httpClient *http.Client, url string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("fleet preflight: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fleet preflight: request %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, nil
	}

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBodyBytes)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("fleet preflight: read %s: %w", url, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, fmt.Errorf("fleet preflight: decode %s: %w", url, err)
	}
	return resp.StatusCode, nil
}
