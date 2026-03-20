package k6

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ---------------------------------------------------------------------------
// Load Tests
// ---------------------------------------------------------------------------

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
