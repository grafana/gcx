package faro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/grafana-app-sdk/logging"
	"k8s.io/client-go/rest"
)

const (
	basePath          = "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app"
	sourcemapBasePath = "/api/plugins/grafana-kowalski-app/resources/api/v1/app"
)

// Client is an HTTP client for the Grafana Faro API.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new Faro client from the given REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("faro: failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// List retrieves all Faro apps.
func (c *Client) List(ctx context.Context) ([]FaroApp, error) {
	log := logging.FromContext(ctx)
	log.Debug("Listing Faro apps", "path", basePath)
	body, statusCode, err := c.doRequest(ctx, http.MethodGet, basePath, nil)
	if err != nil {
		return nil, fmt.Errorf("faro: list apps: %w", err)
	}
	if statusCode >= 400 {
		return nil, fmt.Errorf("faro: list apps: status %d, body: %s", statusCode, string(body))
	}

	var apiApps []faroAppAPI
	if err := json.Unmarshal(body, &apiApps); err != nil {
		return nil, fmt.Errorf("faro: decode apps: %w", err)
	}

	apps := make([]FaroApp, len(apiApps))
	for i, api := range apiApps {
		apps[i] = fromAPI(api)
	}
	log.Debug("Listed Faro apps", "count", len(apps))
	return apps, nil
}

// Get retrieves a Faro app by ID.
func (c *Client) Get(ctx context.Context, id string) (*FaroApp, error) {
	path := fmt.Sprintf("%s/%s", basePath, url.PathEscape(id))

	log := logging.FromContext(ctx)
	log.Debug("Getting Faro app", "id", id, "path", path)
	body, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("faro: get app %s: %w", id, err)
	}
	if statusCode >= 400 {
		return nil, fmt.Errorf("faro: get app %s: status %d, body: %s", id, statusCode, string(body))
	}

	var apiApp faroAppAPI
	if err := json.Unmarshal(body, &apiApp); err != nil {
		return nil, fmt.Errorf("faro: decode app: %w", err)
	}

	app := fromAPI(apiApp)
	log.Debug("Got Faro app", "id", app.ID, "name", app.Name)
	return &app, nil
}

// GetByName retrieves a Faro app by name using client-side filtering.
func (c *Client) GetByName(ctx context.Context, name string) (*FaroApp, error) {
	log := logging.FromContext(ctx)
	log.Debug("Looking up Faro app by name", "name", name)
	apps, err := c.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, app := range apps {
		if app.Name == name {
			log.Debug("Found Faro app by name", "name", name, "id", app.ID)
			return &app, nil
		}
	}

	log.Debug("Faro app not found by name", "name", name, "total_apps", len(apps))
	return nil, fmt.Errorf("faro: app with name %q not found", name)
}

// Create creates a new Faro app.
// ExtraLogLabels and Settings are stripped from the create payload due to Faro API constraints.
// After creation, the app is re-fetched via List to get complete fields (collectEndpointURL, appKey).
func (c *Client) Create(ctx context.Context, app *FaroApp) (*FaroApp, error) {
	log := logging.FromContext(ctx)
	log.Info("Creating Faro app", "name", app.Name)
	apiApp := app.toAPI()
	// Don't send extraLogLabels on create -- the Faro API has a constraint bug
	// that causes 409 errors.
	apiApp.ExtraLogLabels = nil
	// Don't send settings on create -- the Faro API returns 500 if settings are included.
	apiApp.Settings = nil
	log.Debug("Create payload: stripped ExtraLogLabels and Settings (Faro API constraints)")

	body, statusCode, err := c.doRequest(ctx, http.MethodPost, basePath, apiApp)
	if err != nil {
		return nil, fmt.Errorf("faro: create app: %w", err)
	}

	if statusCode == http.StatusConflict {
		return nil, fmt.Errorf("faro: create app conflict: %s", string(body))
	}

	if statusCode >= 400 {
		return nil, fmt.Errorf("faro: create app: status %d, body: %s", statusCode, string(body))
	}

	// After successful creation, fetch via list to get full details (collectEndpointURL, appKey).
	log.Debug("Re-fetching created app via List (create response is incomplete)")
	apps, listErr := c.List(ctx)
	if listErr == nil {
		for _, a := range apps {
			if a.Name == app.Name {
				log.Info("Created Faro app", "name", a.Name, "id", a.ID)
				return &a, nil
			}
		}
		log.Warn("Created app not found in re-fetch; falling back to create response", "name", app.Name)
	} else {
		log.Warn("Re-fetch after create failed; falling back to create response", "error", listErr)
	}

	// Fall back to decoding the create response body.
	var createdAPI faroAppAPI
	if err := json.Unmarshal(body, &createdAPI); err != nil {
		return nil, fmt.Errorf("faro: decode created app: %w", err)
	}

	created := fromAPI(createdAPI)
	log.Info("Created Faro app (from create response)", "name", created.Name, "id", created.ID)
	return &created, nil
}

// Update updates an existing Faro app by ID.
// Settings are stripped from the update payload due to Faro API constraints.
// The ID is included in both URL path and body.
func (c *Client) Update(ctx context.Context, id string, app *FaroApp) (*FaroApp, error) {
	path := fmt.Sprintf("%s/%s", basePath, url.PathEscape(id))

	log := logging.FromContext(ctx)
	log.Info("Updating Faro app", "id", id, "name", app.Name)
	// Faro API requires id in both URL path and body.
	app.ID = id
	apiApp := app.toAPI()
	// Don't send settings on update -- the Faro API returns 500 if settings are included.
	apiApp.Settings = nil
	log.Debug("Update payload: stripped Settings (Faro API constraint)", "id", id)

	body, statusCode, err := c.doRequest(ctx, http.MethodPut, path, apiApp)
	if err != nil {
		return nil, fmt.Errorf("faro: update app %s: %w", id, err)
	}

	if statusCode >= 400 {
		return nil, fmt.Errorf("faro: update app %s: status %d, body: %s", id, statusCode, string(body))
	}

	var updatedAPI faroAppAPI
	if err := json.Unmarshal(body, &updatedAPI); err != nil {
		return nil, fmt.Errorf("faro: decode updated app: %w", err)
	}

	updated := fromAPI(updatedAPI)
	return &updated, nil
}

// Delete deletes a Faro app by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	log := logging.FromContext(ctx)
	log.Info("Deleting Faro app", "id", id)
	path := fmt.Sprintf("%s/%s", basePath, url.PathEscape(id))

	_, statusCode, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("faro: delete app %s: %w", id, err)
	}

	if statusCode >= 400 {
		return fmt.Errorf("faro: delete app %s: status %d", id, statusCode)
	}

	return nil
}

// ListSourcemaps retrieves sourcemaps for a Faro app.
func (c *Client) ListSourcemaps(ctx context.Context, appID string) (json.RawMessage, error) {
	path := fmt.Sprintf("%s/%s/sourcemaps", sourcemapBasePath, url.PathEscape(appID))
	log := logging.FromContext(ctx)
	log.Debug("Listing sourcemaps", "app_id", appID, "path", path)

	body, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("faro: list sourcemaps for app %s: %w", appID, err)
	}

	if statusCode >= 400 {
		return nil, fmt.Errorf("faro: list sourcemaps for app %s: status %d, body: %s", appID, statusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// UploadSourcemap uploads a sourcemap for a Faro app.
func (c *Client) UploadSourcemap(ctx context.Context, appID string, reader io.Reader) (json.RawMessage, error) {
	path := fmt.Sprintf("%s/%s/sourcemaps", sourcemapBasePath, url.PathEscape(appID))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, reader)
	if err != nil {
		return nil, fmt.Errorf("faro: create sourcemap upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("faro: upload sourcemap for app %s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("faro: read sourcemap upload response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("faro: upload sourcemap for app %s: status %d, body: %s", appID, resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// DeleteSourcemap deletes a sourcemap bundle for a Faro app.
func (c *Client) DeleteSourcemap(ctx context.Context, appID, bundleID string) error {
	path := fmt.Sprintf("%s/%s/sourcemaps/%s", sourcemapBasePath, url.PathEscape(appID), url.PathEscape(bundleID))

	_, statusCode, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("faro: delete sourcemap %s for app %s: %w", bundleID, appID, err)
	}

	if statusCode >= 400 {
		return fmt.Errorf("faro: delete sourcemap %s for app %s: status %d", bundleID, appID, statusCode)
	}

	return nil
}

// doRequest builds and executes an HTTP request, returning the response body, status code, and error.
func (c *Client) doRequest(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	log := logging.FromContext(ctx)
	log.Debug("HTTP request", "method", method, "url", c.host+path)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}

	log.Debug("HTTP response", "method", method, "path", path, "status", resp.StatusCode, "body_bytes", len(body))
	return body, resp.StatusCode, nil
}
