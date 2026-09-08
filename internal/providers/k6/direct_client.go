package k6

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/httputils"
)

const (
	// DefaultAPIDomain is the canonical k6 Cloud API endpoint.
	DefaultAPIDomain = "https://api.k6.io"

	authPath = "/v3/account/grafana-app/start"
)

// ReauthFunc refreshes the k6 auth credentials when a cached token is rejected.
// It must perform a fresh /start exchange, persist the new credentials, and
// return them so the caller can update the client's in-memory state.
type ReauthFunc func(ctx context.Context) (token string, orgID int, err error)

// DirectClient talks to api.k6.io directly using an SA-token-exchanged v3 token.
// It does NOT route through the grafana-k6-app plugin proxy — this path exists
// for stacks that cannot use OAuth (CI service accounts, headless automation).
type DirectClient struct {
	*cloudOperations

	apiDomain  string
	logsDomain string
	stackURL   string
	orgID      int
	stackID    int
	token      string
	http       *http.Client
	reauth     ReauthFunc
	mu         sync.Mutex // guards token, orgID, stackID, and stackURL across reauth races
}

// NewDirectClient creates a DirectClient. If apiDomain is empty, DefaultAPIDomain
// is used. If httpClient is nil, a default httputils client is created.
//
// The returned client is not authenticated until Authenticate or SetCachedAuth
// is called. Both Bearer + X-Stack-Id are injected on every subsequent request.
func NewDirectClient(ctx context.Context, apiDomain string, httpClient *http.Client) *DirectClient {
	return newDirectClient(ctx, apiDomain, "", httpClient)
}

func newDirectClient(ctx context.Context, apiDomain, stackURL string, httpClient *http.Client) *DirectClient {
	if apiDomain == "" {
		apiDomain = DefaultAPIDomain
	}
	apiDomain = strings.TrimRight(apiDomain, "/")
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(ctx)
	}
	logsDomain := defaultLogsDomain
	if apiDomain != DefaultAPIDomain {
		logsDomain = apiDomain
	}
	client := &DirectClient{
		apiDomain:  apiDomain,
		logsDomain: logsDomain,
		stackURL:   strings.TrimRight(strings.TrimSpace(stackURL), "/"),
		http:       httpClient,
	}
	client.cloudOperations = &cloudOperations{executor: client}
	return client
}

func (c *DirectClient) selectedStackID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stackID
}

// SetStackURL sets the Grafana stack URL used by ValidateCloudAuth.
func (c *DirectClient) SetStackURL(stackURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stackURL = strings.TrimRight(strings.TrimSpace(stackURL), "/")
}

// Authenticate exchanges a Grafana SA token (glsa_*) for a k6 v3 token by
// calling PUT /v3/account/grafana-app/start. The exchange uses
// X-Grafana-Service-Token; the CAP token does NOT work here (k6 backend
// rejects any header value longer than ~100 chars).
func (c *DirectClient) Authenticate(ctx context.Context, saToken string, stackID int) error {
	stackStr := strconv.Itoa(stackID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.apiDomain+authPath, http.NoBody)
	if err != nil {
		return fmt.Errorf("k6: create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grafana-Service-Token", saToken)
	req.Header.Set("X-Stack-Id", stackStr)
	req.Header.Set("X-Grafana-User", "admin")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("k6: auth request: %w", err)
	}
	response, err := readCloudResponse(resp)
	if err != nil {
		return fmt.Errorf("k6: read auth response: %w", err)
	}

	if response.StatusCode >= 400 {
		return fmt.Errorf("k6: token exchange failed (PUT %s, status %d): %s", authPath, response.StatusCode, string(response.Body))
	}

	var ar authResponse
	if err := json.Unmarshal(response.Body, &ar); err != nil {
		return fmt.Errorf("k6: decode auth response: %w", err)
	}

	orgID, err := strconv.Atoi(ar.OrgID)
	if err != nil {
		return fmt.Errorf("k6: parse organization_id %q: %w", ar.OrgID, err)
	}

	c.mu.Lock()
	c.orgID = orgID
	c.stackID = stackID
	c.token = ar.V3GrafanaToken
	c.mu.Unlock()
	return nil
}

// Token returns the v3 k6 token previously obtained via Authenticate or
// SetCachedAuth. The signature returns an error to satisfy the API interface
// (ProxyClient.Token is lazy and can fail); DirectClient.Token never errors
// in practice but returns errors.New(...) if the client has not been
// authenticated yet, to give callers a clear failure mode.
func (c *DirectClient) Token(_ context.Context) (string, error) {
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok == "" {
		return "", errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
	}
	return tok, nil
}

// orgIDValue returns the cached organization ID, used internally by EnvVar methods.
// The plural form (orgID()) is reserved for ProxyClient's lazy variant.
func (c *DirectClient) orgIDValue() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.orgID
}

// authResponse is the JSON body returned by PUT /v3/account/grafana-app/start.
type authResponse struct {
	OrgID          string `json:"organization_id"`
	V3GrafanaToken string `json:"v3_grafana_token"`
}

// ---------------------------------------------------------------------------
// Auth management helpers
// ---------------------------------------------------------------------------

// SetReauth registers a callback used to refresh credentials when a request is
// rejected with 401. Without it, 401s propagate to the caller as-is. The
// callback is invoked at most once per request — after a successful retry,
// subsequent 401s on the same request are NOT retried.
func (c *DirectClient) SetReauth(fn ReauthFunc) { c.reauth = fn }

// SetCachedAuth populates the client with previously-exchanged credentials,
// skipping the /v3/account/grafana-app/start round-trip. The caller is
// responsible for wiring SetReauth to refresh on 401 if these credentials
// turn out to be stale.
func (c *DirectClient) SetCachedAuth(token string, orgID, stackID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	c.orgID = orgID
	c.stackID = stackID
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// doJSON marshals body to JSON, builds the request via the build closure (so
// the retry path can rebuild it with fresh credentials), and executes it via
// doWithReauth.
func (c *DirectClient) doJSON(ctx context.Context, method, path string, body any) (*http.Response, error) {
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

// doRaw issues a multipart/form-data or application/octet-stream request,
// buffering the body so the retry path can replay it.
func (c *DirectClient) doRaw(ctx context.Context, method, path, contentType string, body io.Reader) (int, []byte, error) {
	var bodyBytes []byte
	if body != nil {
		buf, err := io.ReadAll(body)
		if err != nil {
			return 0, nil, fmt.Errorf("k6: buffer raw request body: %w", err)
		}
		bodyBytes = buf
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

// doWithReauth runs the request; if it 401s and a reauth callback is wired,
// invokes the callback once and retries with refreshed credentials. The
// request is rebuilt for the retry so headers (including Authorization)
// pick up the new token and the body reader is reset.
func (c *DirectClient) doWithReauth(ctx context.Context, build func() (*http.Request, error)) (*http.Response, error) {
	req, err := build()
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || c.reauth == nil {
		return resp, err
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 1<<20)
	_ = resp.Body.Close()

	token, orgID, err := c.reauth(ctx)
	if err != nil {
		return nil, fmt.Errorf("k6: reauth after 401: %w", err)
	}
	c.mu.Lock()
	c.token = token
	c.orgID = orgID
	c.mu.Unlock()

	req2, err := build()
	if err != nil {
		return nil, err
	}
	return c.http.Do(req2)
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

// ListProjects retrieves all projects for the stack.
func (c *DirectClient) ListProjects(ctx context.Context) ([]Project, error) {
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
func (c *DirectClient) GetProject(ctx context.Context, id int) (*Project, error) {
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
func (c *DirectClient) CreateProject(ctx context.Context, name string) (*Project, error) {
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
func (c *DirectClient) UpdateProject(ctx context.Context, id int, name string) error {
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
func (c *DirectClient) DeleteProject(ctx context.Context, id int) error {
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
func (c *DirectClient) GetProjectByName(ctx context.Context, name string) (*Project, error) {
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
func (c *DirectClient) GetLoadTest(ctx context.Context, id int) (*LoadTest, error) {
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
func (c *DirectClient) DeleteLoadTest(ctx context.Context, id int) error {
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
func (c *DirectClient) UpdateLoadTest(ctx context.Context, id int, name, script string) error {
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
func (c *DirectClient) UpdateLoadTestScript(ctx context.Context, id int, script string) error {
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
func (c *DirectClient) GetLoadTestScript(ctx context.Context, id int) (string, error) {
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
func (c *DirectClient) GetLoadTestByName(ctx context.Context, projectID int, name string) (*LoadTest, error) {
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
func (c *DirectClient) ListTestRuns(ctx context.Context, loadTestID int) ([]TestRunStatus, error) {
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
func (c *DirectClient) ListEnvVars(ctx context.Context) ([]EnvVar, error) {
	id := c.orgIDValue()
	if id == 0 {
		return nil, errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
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
func (c *DirectClient) CreateEnvVar(ctx context.Context, name, value, description string) (*EnvVar, error) {
	id := c.orgIDValue()
	if id == 0 {
		return nil, errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
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
func (c *DirectClient) UpdateEnvVar(ctx context.Context, id int, name, value, description string) error {
	orgID := c.orgIDValue()
	if orgID == 0 {
		return errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
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
func (c *DirectClient) DeleteEnvVar(ctx context.Context, id int) error {
	orgID := c.orgIDValue()
	if orgID == 0 {
		return errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
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
func (c *DirectClient) ListSchedules(ctx context.Context) ([]Schedule, error) {
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
func (c *DirectClient) GetSchedule(ctx context.Context, id int) (*Schedule, error) {
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
func (c *DirectClient) CreateSchedule(ctx context.Context, loadTestID int, req ScheduleRequest) (*Schedule, error) {
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
func (c *DirectClient) UpdateScheduleByID(ctx context.Context, id int, req ScheduleRequest) (*Schedule, error) {
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
func (c *DirectClient) DeleteScheduleByLoadTest(ctx context.Context, loadTestID int) error {
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
func (c *DirectClient) ListLoadZones(ctx context.Context) ([]LoadZone, error) {
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
func (c *DirectClient) CreateLoadZone(ctx context.Context, req PLZCreateRequest) (*PLZCreateResponse, error) {
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
func (c *DirectClient) DeleteLoadZone(ctx context.Context, name string) error {
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

// Compile-time assertion: DirectClient must implement API.
var _ API = (*DirectClient)(nil)
