package k6

import (
	"context"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
)

// API is the union of all k6 client operations consumed by gcx commands
// and resource adapters. Implementations:
//   - ProxyClient: routes through the grafana-k6-app plugin proxy (OAuth).
//   - DirectClient: talks to api.k6.io directly with an SA-token-exchanged v3 token.
//
// API is split by domain so commands can depend on a small contract.
type API interface { //nolint:interfacebloat // This union is the compile-time contract for both clients.
	IdentityAPI
	ProjectCRUDAPI
	ProjectMetadataAPI
	LabelKeysAPI
	LoadTestCRUDAPI
	LoadTestOperationsAPI
	TestRunsAPI
	EnvVarsAPI
	SchedulesAPI
	LoadZonesAPI
	AccessAPI
	MetricsAPI
	RunObservabilityAPI
}

type IdentityAPI interface {
	Token(ctx context.Context) (string, error)
	ValidateCloudAuth(ctx context.Context) (*AuthValidation, error)
}

type ProjectCRUDAPI interface {
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id int) (*Project, error)
	CreateProject(ctx context.Context, name string) (*Project, error)
	UpdateProject(ctx context.Context, id int, name string) error
	DeleteProject(ctx context.Context, id int) error
	GetProjectByName(ctx context.Context, name string) (*Project, error)
}

type ProjectMetadataAPI interface {
	ListProjectLimits(ctx context.Context, projectIDs []int, top int) (*ProjectLimitsList, error)
	GetProjectLimits(ctx context.Context, id int) (*ProjectLimits, error)
	UpdateProjectLimits(ctx context.Context, id int, patch ProjectLimitsPatch) error
	ListProjectLabels(ctx context.Context, id int) ([]ProjectLabel, error)
	ReplaceProjectLabels(ctx context.Context, id int, req ProjectLabelPutRequest) ([]ProjectLabel, error)
}

type LabelKeysAPI interface {
	ListLabelKeys(ctx context.Context) ([]LabelKey, error)
	CreateLabelKeys(ctx context.Context, req LabelKeyCreateRequest) ([]LabelKey, error)
	UpdateLabelKey(ctx context.Context, id int, req LabelKeyPatch) (*LabelKey, error)
	DeleteLabelKey(ctx context.Context, id int) error
}

type LoadTestCRUDAPI interface {
	ListLoadTests(ctx context.Context) ([]LoadTest, error)
	ListLoadTestsByProject(ctx context.Context, projectID int) ([]LoadTest, error)
	ListLoadTestsWithLimit(ctx context.Context, limit int) ([]LoadTest, error)
	GetLoadTest(ctx context.Context, id int) (*LoadTest, error)
	GetLoadTestByName(ctx context.Context, projectID int, name string) (*LoadTest, error)
	CreateLoadTest(ctx context.Context, name string, projectID int, script string) (*LoadTest, error)
	UpdateLoadTest(ctx context.Context, id int, name, script string) error
	UpdateLoadTestScript(ctx context.Context, id int, script string) error
	GetLoadTestScript(ctx context.Context, id int) (string, error)
	DeleteLoadTest(ctx context.Context, id int) error
}

type LoadTestOperationsAPI interface {
	MoveLoadTest(ctx context.Context, id, projectID int) error
	StartLoadTest(ctx context.Context, id int, idempotencyKey string) (*TestRun, error)
	GetLoadTestSchedule(ctx context.Context, id int) (*CloudSchedule, error)
	DownloadLoadTestScript(ctx context.Context, id int, accept string) (*ScriptDownload, error)
	ValidateTestOptions(ctx context.Context, req ValidateOptionsRequest) (*ValidateOptionsResult, error)
}

type TestRunsAPI interface {
	ListTestRuns(ctx context.Context, loadTestID int) ([]TestRunStatus, error)
	ListAllTestRuns(ctx context.Context, params TestRunListParams) (*TestRunList, error)
	GetTestRun(ctx context.Context, id int) (*TestRun, error)
	UpdateTestRun(ctx context.Context, id int, note string) error
	DeleteTestRun(ctx context.Context, id int) error
	AbortTestRun(ctx context.Context, id int) error
	GetTestRunDistribution(ctx context.Context, id int) (*TestRunDistribution, error)
	DownloadTestRunScript(ctx context.Context, id int, accept string) (*ScriptDownload, error)
	StarTestRun(ctx context.Context, id int) error
	UnstarTestRun(ctx context.Context, id int) error
}

type EnvVarsAPI interface {
	ListEnvVars(ctx context.Context) ([]EnvVar, error)
	CreateEnvVar(ctx context.Context, name, value, description string) (*EnvVar, error)
	UpdateEnvVar(ctx context.Context, id int, name, value, description string) error
	DeleteEnvVar(ctx context.Context, id int) error
}

type SchedulesAPI interface {
	ListSchedules(ctx context.Context) ([]Schedule, error)
	GetSchedule(ctx context.Context, id int) (*Schedule, error)
	CreateSchedule(ctx context.Context, loadTestID int, req ScheduleRequest) (*Schedule, error)
	UpdateScheduleByID(ctx context.Context, id int, req ScheduleRequest) (*Schedule, error)
	DeleteScheduleByLoadTest(ctx context.Context, loadTestID int) error
	DeleteSchedule(ctx context.Context, id int) error
	ActivateSchedule(ctx context.Context, id int) error
	DeactivateSchedule(ctx context.Context, id int) error
}

type LoadZonesAPI interface {
	ListLoadZones(ctx context.Context) ([]LoadZone, error)
	CreateLoadZone(ctx context.Context, req PLZCreateRequest) (*PLZCreateResponse, error)
	DeleteLoadZone(ctx context.Context, name string) error
}

type AccessAPI interface {
	ListAllowedProjects(ctx context.Context, loadZoneID int) ([]AllowedProject, error)
	UpdateAllowedProjects(ctx context.Context, loadZoneID int, projectIDs []int) error
	ListAllowedLoadZones(ctx context.Context, projectID int) ([]AllowedLoadZone, error)
	UpdateAllowedLoadZones(ctx context.Context, projectID int, loadZoneIDs []int) error
}

type MetricsAPI interface {
	ListTestRunMetrics(ctx context.Context, runID int) (*TestRunMetricsResponse, error)
	ListLoadTestMetrics(ctx context.Context, loadTestID int, selection MetricsRunSelection) (*LoadTestMetricsResponse, error)
	ListTestRunSeries(ctx context.Context, runID int, matches []string) (*prometheus.SeriesResponse, error)
	ListTestRunLabels(ctx context.Context, runID int, matches []string) (*prometheus.LabelsResponse, error)
	ListTestRunLabelValues(ctx context.Context, runID int, label string, matches []string) (*prometheus.LabelsResponse, error)
	QueryTestRunMetrics(ctx context.Context, runID int, req MetricsQueryRequest) (*prometheus.QueryResponse, error)
	QueryLoadTestMetrics(ctx context.Context, loadTestID int, req LoadTestMetricsQueryRequest) (*prometheus.QueryResponse, error)
}

type RunObservabilityAPI interface {
	ListRunLogs(ctx context.Context, runID int, req RunLogsRequest) (*loki.QueryResponse, error)
	ListInsightExecutions(ctx context.Context, runID int) (*InsightExecutionsResponse, error)
	ListInsightAudits(ctx context.Context, runID int, executionID string) (*InsightAuditsResponse, error)
	ListInsightAuditResults(ctx context.Context, runID int, executionID string) (*InsightAuditResultsResponse, error)
}
