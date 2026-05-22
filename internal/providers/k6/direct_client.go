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
	"strconv"
	"strings"

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
	apiDomain string
	orgID     int
	stackID   int
	token     string
	http      *http.Client
	reauth    ReauthFunc
}

// NewDirectClient creates a DirectClient. If apiDomain is empty, DefaultAPIDomain
// is used. If httpClient is nil, a default httputils client is created.
//
// The returned client is not authenticated until Authenticate or SetCachedAuth
// is called. Both Bearer + X-Stack-Id are injected on every subsequent request.
func NewDirectClient(ctx context.Context, apiDomain string, httpClient *http.Client) *DirectClient {
	if apiDomain == "" {
		apiDomain = DefaultAPIDomain
	}
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(ctx)
	}
	return &DirectClient{
		apiDomain: strings.TrimRight(apiDomain, "/"),
		http:      httpClient,
	}
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
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("k6: token exchange failed (PUT %s, status %d): %s", authPath, resp.StatusCode, string(respBody))
	}

	var ar authResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("k6: decode auth response: %w", err)
	}

	orgID, err := strconv.Atoi(ar.OrgID)
	if err != nil {
		return fmt.Errorf("k6: parse organization_id %q: %w", ar.OrgID, err)
	}

	c.orgID = orgID
	c.stackID = stackID
	c.token = ar.V3GrafanaToken
	return nil
}

// Token returns the v3 k6 token previously obtained via Authenticate or
// SetCachedAuth. The signature returns an error to satisfy the API interface
// (ProxyClient.Token is lazy and can fail); DirectClient.Token never errors
// in practice but returns errors.New(...) if the client has not been
// authenticated yet, to give callers a clear failure mode.
func (c *DirectClient) Token(_ context.Context) (string, error) {
	if c.token == "" {
		return "", errors.New("k6: DirectClient not authenticated (call Authenticate or SetCachedAuth first)")
	}
	return c.token, nil
}

// orgIDValue returns the cached organization ID, used internally by EnvVar methods.
// The plural form (orgID()) is reserved for ProxyClient's lazy variant.
func (c *DirectClient) orgIDValue() int { return c.orgID }

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
	build := func() (*http.Request, error) {
		var br io.Reader
		if bodyBytes != nil {
			br = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.apiDomain+path, br)
		if err != nil {
			return nil, fmt.Errorf("k6: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-Stack-Id", strconv.Itoa(c.stackID))
		return req, nil
	}
	return c.doWithReauth(ctx, build)
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
	build := func() (*http.Request, error) {
		var br io.Reader
		if bodyBytes != nil {
			br = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.apiDomain+path, br)
		if err != nil {
			return nil, fmt.Errorf("k6: create raw request: %w", err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-Stack-Id", strconv.Itoa(c.stackID))
		return req, nil
	}
	resp, err := c.doWithReauth(ctx, build)
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
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	token, orgID, err := c.reauth(ctx)
	if err != nil {
		return nil, fmt.Errorf("k6: reauth after 401: %w", err)
	}
	c.token = token
	c.orgID = orgID

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

// ListLoadTestsByProject retrieves load tests filtered by project ID.
// Uses the server-side project_id query parameter to avoid fetching all tests.
func (c *DirectClient) ListLoadTestsByProject(ctx context.Context, projectID int) ([]LoadTest, error) {
	path := fmt.Sprintf(loadTestsPath+"?project_id=%d", projectID)
	return c.listLoadTests(ctx, path, 0)
}

// ListLoadTests retrieves all load tests across all projects, handling pagination.
func (c *DirectClient) ListLoadTests(ctx context.Context) ([]LoadTest, error) {
	return c.listLoadTests(ctx, loadTestsPath, 0)
}

// ListLoadTestsWithLimit retrieves load tests with a server-side limit on the
// number of results. Pass 0 for no limit (fetches all).
func (c *DirectClient) ListLoadTestsWithLimit(ctx context.Context, limit int) ([]LoadTest, error) {
	return c.listLoadTests(ctx, loadTestsPath, limit)
}

// listLoadTests fetches load tests from the given path, paginating through all pages.
// The k6 v6 API uses OData-style pagination with $skip/$top parameters and @count.
// If limit > 0, at most limit items are fetched by setting $top accordingly.
func (c *DirectClient) listLoadTests(ctx context.Context, path string, limit int) ([]LoadTest, error) {
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

// CreateLoadTest creates a new load test via multipart/form-data upload.
//
//nolint:dupl // identical multipart construction; ProxyClient and DirectClient are parallel implementations
func (c *DirectClient) CreateLoadTest(ctx context.Context, name string, projectID int, script string) (*LoadTest, error) {
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

// ---------------------------------------------------------------------------
// Allowed Projects / Load Zones
// ---------------------------------------------------------------------------

// ListAllowedProjects lists the projects allowed to use a load zone.
func (c *DirectClient) ListAllowedProjects(ctx context.Context, loadZoneID int) ([]AllowedProject, error) {
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
func (c *DirectClient) UpdateAllowedProjects(ctx context.Context, loadZoneID int, projectIDs []int) error {
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
func (c *DirectClient) ListAllowedLoadZones(ctx context.Context, projectID int) ([]AllowedLoadZone, error) {
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
func (c *DirectClient) UpdateAllowedLoadZones(ctx context.Context, projectID int, loadZoneIDs []int) error {
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

// Compile-time assertion: DirectClient must implement API.
var _ API = (*DirectClient)(nil)
