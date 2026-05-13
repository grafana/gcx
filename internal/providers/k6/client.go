package k6

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
)

const (
	// pluginProxy forwards requests to api.k6.io with
	// stack-scoped credentials injected by the GCk6 plugin.
	pluginProxyBasePath         = "/api/plugins/k6-app/resources/cloud"
	pluginProxyOrganizationPath = "/api/plugins/k6-app/resources/organization"

	envVarsPathFmt = "/v3/organizations/%d/envvars"
	projectsPath   = "/cloud/v6/projects"
	loadTestsPath  = "/cloud/v6/load_tests"
	schedulesPath  = "/cloud/v6/schedules"
	loadZonesPath  = "/cloud/v6/load_zones"
	plzPath        = "/cloud-resources/v1/load-zones"
)

// Client is an HTTP client for the k6 Cloud API.
// It routes every k6 API call through the grafana-k6-app plugin proxy.
type Client struct {
	grafanaURL string
	proxyBase  string
	http       *http.Client

	cachedToken string // memoized result of /v3/account/me
	cachedOrgID int    // memoized result of /organization, used only by env var methods
}

// NewClient creates a Client that routes every k6 API call through the
// grafana-k6-app plugin proxy on grafanaURL. authClient must carry the
// Grafana auth — typically a client built from a rest.Config wrapped with
// RefreshTransport, so the OAuth bearer is injected (and refreshed before
// expiry) on every request.
func NewClient(ctx context.Context, grafanaURL string, authClient *http.Client) *Client {
	if authClient == nil {
		authClient = httputils.NewDefaultClient(ctx)
	}
	base := strings.TrimRight(grafanaURL, "/")
	return &Client{
		grafanaURL: base,
		proxyBase:  base + pluginProxyBasePath,
		http:       authClient,
	}
}

// orgID hits /organization on the plugin to discover the k6 organization ID
// for legacy APIs. The result is memoised for the life of the client.
func (c *Client) orgID(ctx context.Context) (int, error) {
	if c.cachedOrgID != 0 {
		return c.cachedOrgID, nil
	}

	url := c.grafanaURL + pluginProxyOrganizationPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("k6: create org id request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("k6: fetch org id: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("k6: identity discovery failed (GET %s, status %d): %s", url, resp.StatusCode, string(respBody))
	}

	var orgResp struct {
		OrganizationID int `json:"organization_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&orgResp); err != nil {
		return 0, fmt.Errorf("k6: decode organization response: %w", err)
	}
	c.cachedOrgID = orgResp.OrganizationID
	return c.cachedOrgID, nil
}

// Token returns the user's k6 Personal API token. It is fetched on demand from
// /v3/account/me through the proxy and memoised for the life of the client.
func (c *Client) Token(ctx context.Context) (string, error) {
	if c.cachedToken != "" {
		return c.cachedToken, nil
	}

	resp, err := c.doJSON(ctx, http.MethodGet, "/v3/account/me", nil)
	if err != nil {
		return "", fmt.Errorf("k6: fetch account: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("k6: fetch account: status %d: %s", resp.StatusCode, string(respBody))
	}

	var me struct {
		Token struct {
			Key string `json:"key"`
		} `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return "", fmt.Errorf("k6: decode /v3/account/me: %w", err)
	}
	if me.Token.Key == "" {
		return "", errors.New("k6: /v3/account/me returned empty token.key")
	}
	c.cachedToken = me.Token.Key
	return c.cachedToken, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (c *Client) doJSON(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("k6: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.proxyBase+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("k6: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}

func decodeJSON[T any](resp *http.Response) (T, error) {
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("k6: decode response: %w", err)
	}
	return result, nil
}

func readErrorBody(resp *http.Response) string {
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("(could not read body: %v)", err)
	}
	return string(b)
}

// doRaw performs a raw HTTP request through the plugin proxy.
// Used for multipart/form-data and application/octet-stream requests.
func (c *Client) doRaw(ctx context.Context, method, path, contentType string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.proxyBase+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("k6: create raw request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("k6: raw request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("k6: read raw response: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

// ListProjects retrieves all projects for the stack.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, projectsPath, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list projects: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list projects: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[projectsResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// GetProject retrieves a single project by ID.
func (c *Client) GetProject(ctx context.Context, id int) (*Project, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf(projectsPath+"/%d", id), nil)
	if err != nil {
		return nil, fmt.Errorf("k6: get project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("k6: project %d not found", id)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: get project %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	project, err := decodeJSON[Project](resp)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// CreateProject creates a new project.
func (c *Client) CreateProject(ctx context.Context, name string) (*Project, error) {
	resp, err := c.doJSON(ctx, http.MethodPost, projectsPath, struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return nil, fmt.Errorf("k6: create project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("k6: create project: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	project, err := decodeJSON[Project](resp)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// UpdateProject updates an existing project's name.
func (c *Client) UpdateProject(ctx context.Context, id int, name string) error {
	resp, err := c.doJSON(ctx, http.MethodPatch, fmt.Sprintf(projectsPath+"/%d", id), struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return fmt.Errorf("k6: update project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("k6: update project %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// DeleteProject deletes a project by ID.
func (c *Client) DeleteProject(ctx context.Context, id int) error {
	resp, err := c.doJSON(ctx, http.MethodDelete, fmt.Sprintf(projectsPath+"/%d", id), nil)
	if err != nil {
		return fmt.Errorf("k6: delete project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("k6: delete project %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// GetProjectByName finds a project by name.
func (c *Client) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	projects, err := c.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("k6: project %q not found", name)
}

// ---------------------------------------------------------------------------
// Load Tests
// ---------------------------------------------------------------------------

// ListLoadTestsByProject retrieves load tests filtered by project ID.
// Uses the server-side project_id query parameter to avoid fetching all tests.
func (c *Client) ListLoadTestsByProject(ctx context.Context, projectID int) ([]LoadTest, error) {
	path := fmt.Sprintf(loadTestsPath+"?project_id=%d", projectID)
	return c.listLoadTests(ctx, path, 0)
}

// ListLoadTests retrieves all load tests across all projects, handling pagination.
func (c *Client) ListLoadTests(ctx context.Context) ([]LoadTest, error) {
	return c.listLoadTests(ctx, loadTestsPath, 0)
}

// ListLoadTestsWithLimit retrieves load tests with a server-side limit on the
// number of results. Pass 0 for no limit (fetches all).
func (c *Client) ListLoadTestsWithLimit(ctx context.Context, limit int) ([]LoadTest, error) {
	return c.listLoadTests(ctx, loadTestsPath, limit)
}

// listLoadTests fetches load tests from the given path, paginating through all pages.
// The k6 v6 API uses OData-style pagination with $skip/$top parameters and @count.
// If limit > 0, at most limit items are fetched by setting $top accordingly.
func (c *Client) listLoadTests(ctx context.Context, path string, limit int) ([]LoadTest, error) {
	const defaultPageSize = 100
	var all []LoadTest

	for {
		pageSize := defaultPageSize
		if limit > 0 {
			if remaining := limit - len(all); remaining < pageSize {
				pageSize = remaining
			}
		}

		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		pagePath := fmt.Sprintf("%s%s$skip=%d&$top=%d", path, sep, len(all), pageSize)

		resp, err := c.doJSON(ctx, http.MethodGet, pagePath, nil)
		if err != nil {
			return nil, fmt.Errorf("k6: list load tests: %w", err)
		}

		if resp.StatusCode >= 400 {
			body := readErrorBody(resp)
			resp.Body.Close()
			return nil, fmt.Errorf("k6: list load tests: status %d: %s", resp.StatusCode, body)
		}

		result, err := decodeJSON[loadTestsResponse](resp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		all = append(all, result.Value...)

		// Stop if limit reached.
		if limit > 0 && len(all) >= limit {
			all = all[:limit]
			break
		}

		// Stop if we got fewer results than page size or have fetched all (per @count).
		if len(result.Value) < pageSize || (result.Count > 0 && len(all) >= result.Count) {
			break
		}
	}
	return all, nil
}

// GetLoadTest retrieves a single load test by ID.
func (c *Client) GetLoadTest(ctx context.Context, id int) (*LoadTest, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf(loadTestsPath+"/%d", id), nil)
	if err != nil {
		return nil, fmt.Errorf("k6: get load test: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("k6: load test %d not found", id)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: get load test %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	test, err := decodeJSON[LoadTest](resp)
	if err != nil {
		return nil, err
	}
	return &test, nil
}

// DeleteLoadTest deletes a load test by ID.
func (c *Client) DeleteLoadTest(ctx context.Context, id int) error {
	resp, err := c.doJSON(ctx, http.MethodDelete, fmt.Sprintf(loadTestsPath+"/%d", id), nil)
	if err != nil {
		return fmt.Errorf("k6: delete load test: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("k6: delete load test %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// CreateLoadTest creates a new load test via multipart/form-data upload.
func (c *Client) CreateLoadTest(ctx context.Context, name string, projectID int, script string) (*LoadTest, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("name", name); err != nil {
		return nil, fmt.Errorf("k6: write name field: %w", err)
	}
	part, err := writer.CreateFormFile("script", "script.js")
	if err != nil {
		return nil, fmt.Errorf("k6: create script form file: %w", err)
	}
	if _, err := io.WriteString(part, script); err != nil {
		return nil, fmt.Errorf("k6: write script content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("k6: close multipart writer: %w", err)
	}

	path := fmt.Sprintf(projectsPath+"/%d/load_tests", projectID)
	status, respBody, err := c.doRaw(ctx, http.MethodPost, path, writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, fmt.Errorf("k6: create load test: %w", err)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("k6: create load test: status %d: %s", status, string(respBody))
	}

	var lt LoadTest
	if err := json.Unmarshal(respBody, &lt); err != nil {
		return nil, fmt.Errorf("k6: decode created load test: %w", err)
	}
	return &lt, nil
}

// UpdateLoadTest updates an existing load test's metadata and optionally its script.
func (c *Client) UpdateLoadTest(ctx context.Context, id int, name, script string) error {
	resp, err := c.doJSON(ctx, http.MethodPatch, fmt.Sprintf(loadTestsPath+"/%d", id), struct {
		Name string `json:"name,omitempty"`
	}{Name: name})
	if err != nil {
		return fmt.Errorf("k6: update load test: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("k6: update load test %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	if script != "" {
		return c.UpdateLoadTestScript(ctx, id, script)
	}
	return nil
}

// UpdateLoadTestScript updates only the script of a load test.
func (c *Client) UpdateLoadTestScript(ctx context.Context, id int, script string) error {
	path := fmt.Sprintf(loadTestsPath+"/%d/script", id)
	status, respBody, err := c.doRaw(ctx, http.MethodPut, path, "application/octet-stream", strings.NewReader(script))
	if err != nil {
		return fmt.Errorf("k6: update load test script: %w", err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("k6: update load test script %d: status %d: %s", id, status, string(respBody))
	}
	return nil
}

// GetLoadTestScript fetches the script content of a load test.
func (c *Client) GetLoadTestScript(ctx context.Context, id int) (string, error) {
	path := fmt.Sprintf(loadTestsPath+"/%d/script", id)
	status, body, err := c.doRaw(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return "", fmt.Errorf("k6: get load test script: %w", err)
	}
	if status >= 400 {
		return "", fmt.Errorf("k6: get load test script %d: status %d: %s", id, status, string(body))
	}
	return string(body), nil
}

// GetLoadTestByName finds a load test by name within a project.
func (c *Client) GetLoadTestByName(ctx context.Context, projectID int, name string) (*LoadTest, error) {
	tests, err := c.ListLoadTestsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, t := range tests {
		if t.Name == name {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("k6: load test %q not found in project %d", name, projectID)
}

// ---------------------------------------------------------------------------
// Test Runs
// ---------------------------------------------------------------------------

// ListTestRuns retrieves all test runs for a load test.
func (c *Client) ListTestRuns(ctx context.Context, loadTestID int) ([]TestRunStatus, error) {
	path := fmt.Sprintf(loadTestsPath+"/%d/test_runs", loadTestID)
	resp, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list test runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list test runs for %d: status %d: %s", loadTestID, resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[testRunsResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// ---------------------------------------------------------------------------
// Environment Variables
// ---------------------------------------------------------------------------

// ListEnvVars retrieves all environment variables for the organization.
func (c *Client) ListEnvVars(ctx context.Context) ([]EnvVar, error) {
	id, err := c.orgID(ctx)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf(envVarsPathFmt, id)
	resp, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list env vars: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list env vars: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[envVarsResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.EnvVars, nil
}

// CreateEnvVar creates a new environment variable.
func (c *Client) CreateEnvVar(ctx context.Context, name, value, description string) (*EnvVar, error) {
	id, err := c.orgID(ctx)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf(envVarsPathFmt, id)
	resp, err := c.doJSON(ctx, http.MethodPost, path, envVarRequest{Name: name, Value: value, Description: description})
	if err != nil {
		return nil, fmt.Errorf("k6: create env var: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("k6: create env var: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[envVarResponse](resp)
	if err != nil {
		return nil, err
	}
	return &result.EnvVar, nil
}

// UpdateEnvVar updates an existing environment variable.
func (c *Client) UpdateEnvVar(ctx context.Context, id int, name, value, description string) error {
	orgID, err := c.orgID(ctx)
	if err != nil {
		return err
	}

	path := fmt.Sprintf(envVarsPathFmt+"/%d", orgID, id)
	resp, err := c.doJSON(ctx, http.MethodPatch, path, envVarRequest{Name: name, Value: value, Description: description})
	if err != nil {
		return fmt.Errorf("k6: update env var: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("k6: update env var %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// DeleteEnvVar deletes an environment variable by ID.
func (c *Client) DeleteEnvVar(ctx context.Context, id int) error {
	orgID, err := c.orgID(ctx)
	if err != nil {
		return err
	}

	path := fmt.Sprintf(envVarsPathFmt+"/%d", orgID, id)
	resp, err := c.doJSON(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("k6: delete env var: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("k6: delete env var %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Schedules
// ---------------------------------------------------------------------------

// ListSchedules retrieves all schedules.
func (c *Client) ListSchedules(ctx context.Context) ([]Schedule, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, schedulesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list schedules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list schedules: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[schedulesResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// GetSchedule retrieves a schedule by ID.
func (c *Client) GetSchedule(ctx context.Context, id int) (*Schedule, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf(schedulesPath+"/%d", id), nil)
	if err != nil {
		return nil, fmt.Errorf("k6: get schedule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("k6: schedule %d not found", id)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: get schedule %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	s, err := decodeJSON[Schedule](resp)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateSchedule creates a schedule for a load test.
func (c *Client) CreateSchedule(ctx context.Context, loadTestID int, req ScheduleRequest) (*Schedule, error) {
	path := fmt.Sprintf(loadTestsPath+"/%d/schedule", loadTestID)
	resp, err := c.doJSON(ctx, http.MethodPost, path, req)
	if err != nil {
		return nil, fmt.Errorf("k6: create schedule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("k6: create schedule: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	s, err := decodeJSON[Schedule](resp)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateScheduleByID updates a schedule by its ID.
func (c *Client) UpdateScheduleByID(ctx context.Context, id int, req ScheduleRequest) (*Schedule, error) {
	resp, err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf(schedulesPath+"/%d", id), req)
	if err != nil {
		return nil, fmt.Errorf("k6: update schedule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("k6: update schedule %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	if resp.StatusCode == http.StatusNoContent {
		return c.GetSchedule(ctx, id)
	}

	s, err := decodeJSON[Schedule](resp)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteScheduleByLoadTest deletes the schedule for a load test.
func (c *Client) DeleteScheduleByLoadTest(ctx context.Context, loadTestID int) error {
	path := fmt.Sprintf(loadTestsPath+"/%d/schedule", loadTestID)
	resp, err := c.doJSON(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("k6: delete schedule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("k6: delete schedule for load test %d: status %d: %s", loadTestID, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Load Zones
// ---------------------------------------------------------------------------

// ListLoadZones retrieves all load zones for the stack.
func (c *Client) ListLoadZones(ctx context.Context) ([]LoadZone, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, loadZonesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list load zones: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list load zones: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[loadZonesResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// CreateLoadZone registers a Private Load Zone.
func (c *Client) CreateLoadZone(ctx context.Context, req PLZCreateRequest) (*PLZCreateResponse, error) {
	resp, err := c.doJSON(ctx, http.MethodPost, plzPath, req)
	if err != nil {
		return nil, fmt.Errorf("k6: create load zone: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("k6: create load zone: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[PLZCreateResponse](resp)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteLoadZone deregisters a Private Load Zone by name.
func (c *Client) DeleteLoadZone(ctx context.Context, name string) error {
	resp, err := c.doJSON(ctx, http.MethodDelete, plzPath+"/"+url.PathEscape(name), nil)
	if err != nil {
		return fmt.Errorf("k6: delete load zone: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("k6: delete load zone %q: status %d: %s", name, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Allowed Projects / Load Zones
// ---------------------------------------------------------------------------

// ListAllowedProjects lists the projects allowed to use a load zone.
func (c *Client) ListAllowedProjects(ctx context.Context, loadZoneID int) ([]AllowedProject, error) {
	path := fmt.Sprintf(loadZonesPath+"/%d/allowed_projects", loadZoneID)
	resp, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list allowed projects: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list allowed projects: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[allowedProjectsResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// UpdateAllowedProjects sets the projects allowed to use a load zone.
func (c *Client) UpdateAllowedProjects(ctx context.Context, loadZoneID int, projectIDs []int) error {
	path := fmt.Sprintf(loadZonesPath+"/%d/allowed_projects", loadZoneID)
	body := struct {
		ProjectIDs []int `json:"project_ids"`
	}{ProjectIDs: projectIDs}
	resp, err := c.doJSON(ctx, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("k6: update allowed projects: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("k6: update allowed projects: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// ListAllowedLoadZones lists the load zones allowed for a project.
func (c *Client) ListAllowedLoadZones(ctx context.Context, projectID int) ([]AllowedLoadZone, error) {
	path := fmt.Sprintf(projectsPath+"/%d/allowed_load_zones", projectID)
	resp, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list allowed load zones: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list allowed load zones: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[allowedLoadZonesResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// UpdateAllowedLoadZones sets the load zones allowed for a project.
func (c *Client) UpdateAllowedLoadZones(ctx context.Context, projectID int, loadZoneIDs []int) error {
	path := fmt.Sprintf(projectsPath+"/%d/allowed_load_zones", projectID)
	body := struct {
		LoadZoneIDs []int `json:"load_zone_ids"`
	}{LoadZoneIDs: loadZoneIDs}
	resp, err := c.doJSON(ctx, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("k6: update allowed load zones: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("k6: update allowed load zones: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}
