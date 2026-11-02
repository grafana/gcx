package k6

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/httputils"
)

const (
	// pluginProxy forwards requests to api.k6.io with
	// stack-scoped credentials injected by the Grafana Cloud k6 plugin.
	pluginProxyBasePath         = "/api/plugins/k6-app/resources/cloud"
	pluginProxyOrganizationPath = "/api/plugins/k6-app/resources/organization"

	envVarsPathFmt = "/v3/organizations/%d/envvars"
	projectsPath   = "/cloud/v6/projects"
	loadTestsPath  = "/cloud/v6/load_tests"
	schedulesPath  = "/cloud/v6/schedules"
	loadZonesPath  = "/cloud/v6/load_zones"
	plzPath        = "/cloud-resources/v1/load-zones"
)

// ProxyClient is an HTTP client for the k6 Cloud API.
// It routes every k6 API call through the grafana-k6-app plugin proxy.
type ProxyClient struct {
	*cloudOperations

	host string
	http *http.Client

	stackURL   string
	stackID    int
	apiDomain  string
	logsDomain string
	directHTTP *http.Client

	mu          sync.Mutex
	cachedToken string // memoized result of /v3/account/me
	cachedOrgID int    // memoized result of /organization, used only by env var methods
}

// NewProxyClient creates a ProxyClient that routes every k6 API call through the
// grafana-k6-app plugin proxy on host. authClient must carry the
// Grafana auth — typically a client built from a rest.Config wrapped with
// RefreshTransport, so the OAuth bearer is injected (and refreshed before
// expiry) on every request.
func NewProxyClient(ctx context.Context, host string, authClient *http.Client) *ProxyClient {
	return newProxyClient(ctx, host, host, 0, DefaultAPIDomain, authClient, nil)
}

func newProxyClient(
	ctx context.Context,
	host string,
	stackURL string,
	stackID int,
	apiDomain string,
	authClient *http.Client,
	directHTTP *http.Client,
) *ProxyClient {
	if authClient == nil {
		authClient = httputils.NewDefaultClient(ctx)
	}
	if directHTTP == nil {
		directHTTP = httputils.NewDefaultClient(ctx)
	}
	base := strings.TrimRight(host, "/")
	client := &ProxyClient{
		host:       base,
		http:       authClient,
		stackURL:   strings.TrimRight(strings.TrimSpace(stackURL), "/"),
		stackID:    stackID,
		apiDomain:  normalizeAPIDomain(apiDomain),
		logsDomain: defaultLogsDomain,
		directHTTP: directHTTP,
	}
	client.cloudOperations = &cloudOperations{executor: client}
	return client
}

func (c *ProxyClient) selectedStackID() int { return c.stackID }

// orgID hits /organization on the plugin to discover the k6 organization ID
// for legacy APIs. The result is memoised for the life of the client.
func (c *ProxyClient) orgID(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedOrgID != 0 {
		return c.cachedOrgID, nil
	}

	url := c.host + pluginProxyOrganizationPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("k6: create org id request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("k6: fetch org id: %w", err)
	}
	response, err := readCloudResponse(resp)
	if err != nil {
		return 0, fmt.Errorf("k6: read organization response: %w", err)
	}

	if response.StatusCode >= 400 {
		return 0, fmt.Errorf("k6: identity discovery failed (GET %s, status %d): %s", url, response.StatusCode, string(response.Body))
	}

	var orgResp struct {
		OrganizationID int `json:"organization_id"`
	}
	if err := json.Unmarshal(response.Body, &orgResp); err != nil {
		return 0, fmt.Errorf("k6: decode organization response: %w", err)
	}
	c.cachedOrgID = orgResp.OrganizationID
	return c.cachedOrgID, nil
}

// Token returns the user's k6 Personal API token. It is fetched on demand from
// /v3/account/me through the proxy and memoised for the life of the client.
func (c *ProxyClient) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.cachedToken
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
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
	c.mu.Lock()
	if c.cachedToken == "" {
		c.cachedToken = me.Token.Key
	}
	cached = c.cachedToken
	c.mu.Unlock()
	return cached, nil
}

func (c *ProxyClient) invalidateToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedToken == token {
		c.cachedToken = ""
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (c *ProxyClient) doJSON(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("k6: marshal request body: %w", err)
		}
		bodyBytes = b
	}
	response, err := c.doCloud(ctx, cloudRequest{
		Target:      cloudTargetCloud,
		Auth:        cloudAuthConfigured,
		Method:      method,
		Path:        path,
		Body:        bodyBytes,
		ContentType: "application/json",
		Accept:      "application/json",
	})
	if err != nil {
		return nil, err
	}
	return asHTTPResponse(response), nil
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
func (c *ProxyClient) doRaw(ctx context.Context, method, path, contentType string, body io.Reader) (int, []byte, error) {
	var bodyBytes []byte
	if body != nil {
		buffered, err := io.ReadAll(body)
		if err != nil {
			return 0, nil, fmt.Errorf("k6: buffer raw request body: %w", err)
		}
		bodyBytes = buffered
	}
	response, err := c.doCloud(ctx, cloudRequest{
		Target:      cloudTargetCloud,
		Auth:        cloudAuthConfigured,
		Method:      method,
		Path:        path,
		Body:        bodyBytes,
		ContentType: contentType,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("k6: raw request: %w", err)
	}
	return response.StatusCode, response.Body, nil
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

// ListProjects retrieves all projects for the stack.
func (c *ProxyClient) ListProjects(ctx context.Context) ([]Project, error) {
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
func (c *ProxyClient) GetProject(ctx context.Context, id int) (*Project, error) {
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
func (c *ProxyClient) CreateProject(ctx context.Context, name string) (*Project, error) {
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
func (c *ProxyClient) UpdateProject(ctx context.Context, id int, name string) error {
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
func (c *ProxyClient) DeleteProject(ctx context.Context, id int) error {
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
func (c *ProxyClient) GetProjectByName(ctx context.Context, name string) (*Project, error) {
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

// GetLoadTest retrieves a single load test by ID.
func (c *ProxyClient) GetLoadTest(ctx context.Context, id int) (*LoadTest, error) {
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
func (c *ProxyClient) DeleteLoadTest(ctx context.Context, id int) error {
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

// UpdateLoadTest updates an existing load test's metadata and optionally its script.
func (c *ProxyClient) UpdateLoadTest(ctx context.Context, id int, name, script string) error {
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
func (c *ProxyClient) UpdateLoadTestScript(ctx context.Context, id int, script string) error {
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
func (c *ProxyClient) GetLoadTestScript(ctx context.Context, id int) (string, error) {
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
func (c *ProxyClient) GetLoadTestByName(ctx context.Context, projectID int, name string) (*LoadTest, error) {
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
func (c *ProxyClient) ListTestRuns(ctx context.Context, loadTestID int) ([]TestRunStatus, error) {
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
func (c *ProxyClient) ListEnvVars(ctx context.Context) ([]EnvVar, error) {
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
func (c *ProxyClient) CreateEnvVar(ctx context.Context, name, value, description string) (*EnvVar, error) {
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
func (c *ProxyClient) UpdateEnvVar(ctx context.Context, id int, name, value, description string) error {
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
func (c *ProxyClient) DeleteEnvVar(ctx context.Context, id int) error {
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
func (c *ProxyClient) ListSchedules(ctx context.Context) ([]Schedule, error) {
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
func (c *ProxyClient) GetSchedule(ctx context.Context, id int) (*Schedule, error) {
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
func (c *ProxyClient) CreateSchedule(ctx context.Context, loadTestID int, req ScheduleRequest) (*Schedule, error) {
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
func (c *ProxyClient) UpdateScheduleByID(ctx context.Context, id int, req ScheduleRequest) (*Schedule, error) {
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
func (c *ProxyClient) DeleteScheduleByLoadTest(ctx context.Context, loadTestID int) error {
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
func (c *ProxyClient) ListLoadZones(ctx context.Context) ([]LoadZone, error) {
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
func (c *ProxyClient) CreateLoadZone(ctx context.Context, req PLZCreateRequest) (*PLZCreateResponse, error) {
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
func (c *ProxyClient) DeleteLoadZone(ctx context.Context, name string) error {
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

// Compile-time assertion: ProxyClient must implement API.
var _ API = (*ProxyClient)(nil)
