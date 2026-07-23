package experiments

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const basePath = "/eval/experiments"
const testSuitesBasePath = "/eval/test-suites"
const testCaseTrialsBasePath = "/eval/test-case-trials"

// ErrNotFound is returned by resource lookup and mutation methods when the
// server responds with 404 so callers can distinguish a missing resource from
// other API errors. Callers wrap it with the relevant resource ID.
var ErrNotFound = errors.New("not found")

// Client wraps the Agent Observability plugin proxy with experiment-specific endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new experiments client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns experiments, paginated. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Experiment, error) {
	return aio11yhttp.ListAll[Experiment](ctx, c.base, basePath, nil, limit)
}

// ListSuites returns test suites, paginated. Pass 0 for no limit.
func (c *Client) ListSuites(ctx context.Context, limit int) ([]TestSuite, error) {
	return aio11yhttp.ListAll[TestSuite](ctx, c.base, testSuitesBasePath, nil, limit)
}

// GetSuite returns a single test suite with its versions.
func (c *Client) GetSuite(ctx context.Context, suiteID string) (*TestSuite, error) {
	suite, err := aio11yhttp.DoJSONNotFound[any, TestSuite](ctx, c.base, http.MethodGet, testSuitesBasePath+"/"+url.PathEscape(suiteID), nil,
		fmt.Errorf("%s: %w", suiteID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &suite, nil
}

// CreateSuite creates a new test suite.
func (c *Client) CreateSuite(ctx context.Context, suite *TestSuite) (*TestSuite, error) {
	created, err := aio11yhttp.DoJSON[TestSuite, TestSuite](ctx, c.base, http.MethodPost, testSuitesBasePath, suite, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateSuite patches a test suite.
func (c *Client) UpdateSuite(ctx context.Context, suiteID string, req *UpdateTestSuiteRequest) (*TestSuite, error) {
	suite, err := aio11yhttp.DoJSONNotFound[UpdateTestSuiteRequest, TestSuite](ctx, c.base, http.MethodPatch, testSuitesBasePath+"/"+url.PathEscape(suiteID), req,
		fmt.Errorf("%s: %w", suiteID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &suite, nil
}

type UpdateTestSuiteRequest struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

type CreateTestSuiteVersionRequest struct {
	Changelog  string `json:"changelog,omitempty"`
	EmptyDraft bool   `json:"empty_draft,omitempty"`
}

// CreateSuiteVersion creates a draft version for a suite.
func (c *Client) CreateSuiteVersion(ctx context.Context, suiteID string, req *CreateTestSuiteVersionRequest) (*TestSuiteVersion, error) {
	path := testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions"
	version, err := aio11yhttp.DoJSON[CreateTestSuiteVersionRequest, TestSuiteVersion](ctx, c.base, http.MethodPost, path, req, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// PublishSuiteVersion publishes a draft suite version.
func (c *Client) PublishSuiteVersion(ctx context.Context, suiteID, version string) (*TestSuiteVersion, error) {
	escapedVersion := strings.ReplaceAll(url.PathEscape(version), ":", "%3A")
	path := testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions/" + escapedVersion + ":publish"
	published, err := aio11yhttp.DoJSONNotFound[any, TestSuiteVersion](ctx, c.base, http.MethodPost, path, nil,
		fmt.Errorf("%s/%s: %w", suiteID, version, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &published, nil
}

func testCasesPath(suiteID, version string) string {
	return testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions/" + url.PathEscape(version) + "/test-cases"
}

// ListCases returns test cases for a suite version.
func (c *Client) ListCases(ctx context.Context, suiteID, version string, limit int) ([]TestCase, error) {
	return aio11yhttp.ListAll[TestCase](ctx, c.base, testCasesPath(suiteID, version), nil, limit)
}

// GetCase returns a single test case.
func (c *Client) GetCase(ctx context.Context, suiteID, version, testCaseID string) (*TestCase, error) {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	tc, err := aio11yhttp.DoJSONNotFound[any, TestCase](ctx, c.base, http.MethodGet, path, nil,
		fmt.Errorf("%s: %w", testCaseID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &tc, nil
}

// UpsertCase creates or replaces a test case in a mutable suite version.
func (c *Client) UpsertCase(ctx context.Context, suiteID, version string, tc *TestCase) (*TestCase, error) {
	out, err := aio11yhttp.DoJSON[TestCase, TestCase](ctx, c.base, http.MethodPost, testCasesPath(suiteID, version), tc, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchCase patches a test case in a mutable suite version.
func (c *Client) PatchCase(ctx context.Context, suiteID, version, testCaseID string, patch map[string]any) (*TestCase, error) {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	out, err := aio11yhttp.DoJSONNotFound[map[string]any, TestCase](ctx, c.base, http.MethodPatch, path, &patch,
		fmt.Errorf("%s: %w", testCaseID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCase deletes a test case from a mutable suite version.
func (c *Client) DeleteCase(ctx context.Context, suiteID, version, testCaseID string) error {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	return aio11yhttp.DoStatusNotFound[any](ctx, c.base, http.MethodDelete, path, nil,
		fmt.Errorf("%s: %w", testCaseID, ErrNotFound), http.StatusOK, http.StatusNoContent)
}

// Get returns a single experiment by ID.
func (c *Client) Get(ctx context.Context, runID string) (*Experiment, error) {
	exp, err := aio11yhttp.DoJSONNotFound[any, Experiment](ctx, c.base, http.MethodGet, basePath+"/"+url.PathEscape(runID), nil,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &exp, nil
}

// ListTrials returns test case trials for an experiment.
func (c *Client) ListTrials(ctx context.Context, experimentID string, limit int) ([]TestCaseTrial, error) {
	path := basePath + "/" + url.PathEscape(experimentID) + "/trials"
	return aio11yhttp.ListAll[TestCaseTrial](ctx, c.base, path, nil, limit)
}

// CreateTrial creates or upserts a test case trial for an experiment.
func (c *Client) CreateTrial(ctx context.Context, experimentID string, trial *TestCaseTrial) (*TestCaseTrial, error) {
	path := basePath + "/" + url.PathEscape(experimentID) + "/trials"
	out, err := aio11yhttp.DoJSON[TestCaseTrial, TestCaseTrial](ctx, c.base, http.MethodPost, path, trial, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTrial returns a single test case trial.
func (c *Client) GetTrial(ctx context.Context, trialID string) (*TestCaseTrial, error) {
	out, err := aio11yhttp.DoJSONNotFound[any, TestCaseTrial](ctx, c.base, http.MethodGet, testCaseTrialsBasePath+"/"+url.PathEscape(trialID), nil,
		fmt.Errorf("%s: %w", trialID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTrial patches a single test case trial.
func (c *Client) UpdateTrial(ctx context.Context, trialID string, req *UpdateTrialRequest) (*TestCaseTrial, error) {
	out, err := aio11yhttp.DoJSONNotFound[UpdateTrialRequest, TestCaseTrial](ctx, c.base, http.MethodPatch, testCaseTrialsBasePath+"/"+url.PathEscape(trialID), req,
		fmt.Errorf("%s: %w", trialID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTrialScores returns scores associated with a test case trial.
func (c *Client) ListTrialScores(ctx context.Context, trialID string, limit int) ([]ScoreItem, error) {
	path := testCaseTrialsBasePath + "/" + url.PathEscape(trialID) + "/scores"
	return aio11yhttp.ListAll[ScoreItem](ctx, c.base, path, nil, limit)
}

// ListTrialArtifacts returns artifacts associated with a test case trial.
func (c *Client) ListTrialArtifacts(ctx context.Context, trialID string, limit int) ([]Artifact, error) {
	path := testCaseTrialsBasePath + "/" + url.PathEscape(trialID) + "/artifacts"
	return aio11yhttp.ListAll[Artifact](ctx, c.base, path, nil, limit)
}

// Create creates a new experiment.
func (c *Client) Create(ctx context.Context, exp *Experiment) (*Experiment, error) {
	created, err := aio11yhttp.DoJSON[Experiment, Experiment](ctx, c.base, http.MethodPost, basePath, exp, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update sends a partial PATCH against an existing experiment.
func (c *Client) Update(ctx context.Context, runID string, req *UpdateRequest) (*Experiment, error) {
	exp, err := aio11yhttp.DoJSONNotFound[UpdateRequest, Experiment](ctx, c.base, http.MethodPatch, basePath+"/"+url.PathEscape(runID), req,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &exp, nil
}

// Cancel transitions a running experiment to a canceled state.
//
// The plugin proxy matches the `:cancel` suffix on the run ID segment
// (single-segment path), not `/cancel`. url.PathEscape does not escape
// `:` (it's an allowed sub-delim in path segments), which would make the
// route ambiguous if a runID ever contained a literal colon, so we escape
// it manually before appending the action suffix.
func (c *Client) Cancel(ctx context.Context, runID string) error {
	escaped := strings.ReplaceAll(url.PathEscape(runID), ":", "%3A")
	return aio11yhttp.DoStatusNotFound[any](ctx, c.base, http.MethodPost, basePath+"/"+escaped+":cancel", nil,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK, http.StatusNoContent, http.StatusAccepted)
}

// ListScores returns scores associated with a single experiment run.
func (c *Client) ListScores(ctx context.Context, runID string, limit int) ([]ScoreItem, error) {
	path := basePath + "/" + url.PathEscape(runID) + "/scores"
	return aio11yhttp.ListAll[ScoreItem](ctx, c.base, path, nil, limit)
}

// GetReport returns the aggregate report for an experiment run.
func (c *Client) GetReport(ctx context.Context, runID string) (*ExperimentReport, error) {
	report, err := aio11yhttp.DoJSONNotFound[any, ExperimentReport](ctx, c.base, http.MethodGet, basePath+"/"+url.PathEscape(runID)+"/report", nil,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &report, nil
}
