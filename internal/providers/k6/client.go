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
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAPIDomain is the default K6 Cloud API domain.
	DefaultAPIDomain = "https://api.k6.io"

	authPath       = "/v3/account/grafana-app/start"
	envVarsPathFmt = "/v3/organizations/%d/envvars"
	projectsPath   = "/cloud/v6/projects"
	loadTestsPath  = "/cloud/v6/load_tests"
	schedulesPath  = "/cloud/v6/schedules"
	loadZonesPath  = "/cloud/v6/load_zones"
	plzPath        = "/cloud-resources/v1/load-zones"
)

// Client is an HTTP client for the K6 Cloud API.
// It must be authenticated before use by calling Authenticate.
type Client struct {
	apiDomain string
	orgID     int
	stackID   int
	token     string
	http      *http.Client
}

// NewClient creates a new K6 Cloud client with the given API domain.
func NewClient(apiDomain string) *Client {
	if apiDomain == "" {
		apiDomain = DefaultAPIDomain
	}
	return &Client{
		apiDomain: strings.TrimRight(apiDomain, "/"),
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Authenticate exchanges a Grafana Cloud AP token for a k6 API token.
// The grafanaUser is typically "admin" for service account tokens.
func (c *Client) Authenticate(ctx context.Context, apToken string, stackID int) error {
	stackStr := strconv.Itoa(stackID)

	body, err := json.Marshal(struct{}{})
	if err != nil {
		return fmt.Errorf("k6: marshal auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.apiDomain+authPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("k6: create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grafana-Key", apToken)
	req.Header.Set("X-Stack-Id", stackStr)
	req.Header.Set("X-Grafana-User", "admin")
	req.Header.Set("X-Grafana-Service-Token", apToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("k6: auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("k6: token exchange failed (PUT %s, status %d): %s", authPath, resp.StatusCode, string(respBody))
	}

	var authResp authResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("k6: decode auth response: %w", err)
	}

	orgID, err := strconv.Atoi(authResp.OrgID)
	if err != nil {
		return fmt.Errorf("k6: parse organization_id %q: %w", authResp.OrgID, err)
	}

	c.orgID = orgID
	c.stackID = stackID
	c.token = authResp.V3GrafanaToken
	return nil
}

// OrgID returns the k6 organization ID after authentication.
func (c *Client) OrgID() int { return c.orgID }

// Token returns the authenticated k6 v3 token.
func (c *Client) Token() string { return c.token }

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

	req, err := http.NewRequestWithContext(ctx, method, c.apiDomain+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("k6: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Stack-Id", strconv.Itoa(c.stackID))

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

// doRaw performs a raw HTTP request with Bearer + X-Stack-Id headers.
// Used for multipart/form-data and application/octet-stream requests.
func (c *Client) doRaw(ctx context.Context, method, path, contentType string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.apiDomain+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("k6: create raw request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Stack-Id", strconv.Itoa(c.stackID))

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
	projects, err := c.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("k6: project %d not found", id)
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

// ListAllLoadTests retrieves all load tests across all projects.
// This is an alias for ListLoadTests for clarity when both variants are used.
func (c *Client) ListAllLoadTests(ctx context.Context) ([]LoadTest, error) {
	return c.ListLoadTests(ctx)
}

// ListLoadTestsByProject retrieves load tests filtered by project ID.
func (c *Client) ListLoadTestsByProject(ctx context.Context, projectID int) ([]LoadTest, error) {
	all, err := c.ListLoadTests(ctx)
	if err != nil {
		return nil, err
	}
	var filtered []LoadTest
	for _, t := range all {
		if t.ProjectID == projectID {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// ListLoadTests retrieves all load tests across all projects.
func (c *Client) ListLoadTests(ctx context.Context) ([]LoadTest, error) {
	resp, err := c.doJSON(ctx, http.MethodGet, loadTestsPath, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list load tests: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list load tests: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	result, err := decodeJSON[loadTestsResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
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
	if c.orgID == 0 {
		return nil, errors.New("k6: client not authenticated (no org ID)")
	}

	path := fmt.Sprintf(envVarsPathFmt, c.orgID)
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
	if c.orgID == 0 {
		return nil, errors.New("k6: client not authenticated (no org ID)")
	}

	path := fmt.Sprintf(envVarsPathFmt, c.orgID)
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
	if c.orgID == 0 {
		return errors.New("k6: client not authenticated (no org ID)")
	}

	path := fmt.Sprintf(envVarsPathFmt+"/%d", c.orgID, id)
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
	if c.orgID == 0 {
		return errors.New("k6: client not authenticated (no org ID)")
	}

	path := fmt.Sprintf(envVarsPathFmt+"/%d", c.orgID, id)
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
	resp, err := c.doJSON(ctx, http.MethodDelete, plzPath+"/"+name, nil)
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
