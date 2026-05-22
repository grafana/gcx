package k6

import "context"

// API is the union of all k6 client operations consumed by gcx commands
// and resource adapters. Implementations:
//   - ProxyClient: routes through the grafana-k6-app plugin proxy (OAuth).
//   - DirectClient: talks to api.k6.io directly with an SA-token-exchanged v3 token.
//
//nolint:interfacebloat // single source of truth for both client implementations;
// the alternative is two parallel call sites everywhere.
type API interface {
	// Identity
	Token(ctx context.Context) (string, error)

	// Projects
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id int) (*Project, error)
	CreateProject(ctx context.Context, name string) (*Project, error)
	UpdateProject(ctx context.Context, id int, name string) error
	DeleteProject(ctx context.Context, id int) error
	GetProjectByName(ctx context.Context, name string) (*Project, error)

	// Load tests
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

	// Test runs
	ListTestRuns(ctx context.Context, loadTestID int) ([]TestRunStatus, error)

	// Env vars
	ListEnvVars(ctx context.Context) ([]EnvVar, error)
	CreateEnvVar(ctx context.Context, name, value, description string) (*EnvVar, error)
	UpdateEnvVar(ctx context.Context, id int, name, value, description string) error
	DeleteEnvVar(ctx context.Context, id int) error

	// Schedules
	ListSchedules(ctx context.Context) ([]Schedule, error)
	GetSchedule(ctx context.Context, id int) (*Schedule, error)
	CreateSchedule(ctx context.Context, loadTestID int, req ScheduleRequest) (*Schedule, error)
	UpdateScheduleByID(ctx context.Context, id int, req ScheduleRequest) (*Schedule, error)
	DeleteScheduleByLoadTest(ctx context.Context, loadTestID int) error

	// Load zones
	ListLoadZones(ctx context.Context) ([]LoadZone, error)
	CreateLoadZone(ctx context.Context, req PLZCreateRequest) (*PLZCreateResponse, error)
	DeleteLoadZone(ctx context.Context, name string) error

	// Allowed projects / load zones
	ListAllowedProjects(ctx context.Context, loadZoneID int) ([]AllowedProject, error)
	UpdateAllowedProjects(ctx context.Context, loadZoneID int, projectIDs []int) error
	ListAllowedLoadZones(ctx context.Context, projectID int) ([]AllowedLoadZone, error)
	UpdateAllowedLoadZones(ctx context.Context, projectID int, loadZoneIDs []int) error
}
