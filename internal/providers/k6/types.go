package k6

import "strconv"

// ---------- ResourceIdentity implementations ----------

func (p Project) GetResourceName() string      { return strconv.Itoa(p.ID) }
func (p *Project) SetResourceName(name string) { p.ID, _ = strconv.Atoi(name) }

func (lt LoadTest) GetResourceName() string      { return strconv.Itoa(lt.ID) }
func (lt *LoadTest) SetResourceName(name string) { lt.ID, _ = strconv.Atoi(name) }

func (s Schedule) GetResourceName() string      { return strconv.Itoa(s.ID) }
func (s *Schedule) SetResourceName(name string) { s.ID, _ = strconv.Atoi(name) }

func (ev EnvVar) GetResourceName() string      { return strconv.Itoa(ev.ID) }
func (ev *EnvVar) SetResourceName(name string) { ev.ID, _ = strconv.Atoi(name) }

func (lz LoadZone) GetResourceName() string      { return lz.Name }
func (lz *LoadZone) SetResourceName(name string) { lz.Name = name }

// Project represents a K6 Cloud project (v6 API).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Project struct {
	ID               int    `json:"id,omitempty"`
	Name             string `json:"name"`
	IsDefault        bool   `json:"is_default,omitempty"`
	GrafanaFolderUID string `json:"grafana_folder_uid,omitempty"`
	Created          string `json:"created,omitempty"`
	Updated          string `json:"updated,omitempty"`
}

// LoadTest represents a K6 load test (v6 API).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type LoadTest struct {
	ID        int    `json:"id,omitempty"`
	Name      string `json:"name"`
	ProjectID int    `json:"project_id"`
	Script    string `json:"script,omitempty"`
	Created   string `json:"created,omitempty"`
	Updated   string `json:"updated,omitempty"`
}

// EnvVar represents a K6 Cloud environment variable.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type EnvVar struct {
	ID          int    `json:"id,omitempty"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// TestRunStatus represents the status of a k6 test run.
type TestRunStatus struct {
	ID           int    `json:"id,omitempty"`
	LoadTestID   int    `json:"load_test_id"`
	Status       string `json:"status"`
	ResultStatus int    `json:"result_status"`
	Created      string `json:"created,omitempty"`
	Ended        string `json:"ended,omitempty"`
	ReferenceID  string `json:"reference_id,omitempty"`
}

// authResponse is the response from K6 authentication.
type authResponse struct {
	OrgID          string `json:"organization_id"`
	V3GrafanaToken string `json:"v3_grafana_token"`
}

// projectsResponse is the response from listing projects.
type projectsResponse struct {
	Value []Project `json:"value"`
}

// loadTestsResponse is the response from listing load tests.
type loadTestsResponse struct {
	Value []LoadTest `json:"value"`
	Count int        `json:"@count,omitempty"`
}

// envVarsResponse is the response from listing environment variables.
type envVarsResponse struct {
	EnvVars []EnvVar `json:"envvars"`
}

// envVarResponse is the response from creating an environment variable.
type envVarResponse struct {
	EnvVar EnvVar `json:"envvar"`
}

// envVarRequest is the request body for creating or updating an environment variable.
type envVarRequest struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// testRunsResponse is the response from listing test runs.
type testRunsResponse struct {
	Value []TestRunStatus `json:"value"`
}

// Schedule represents a K6 schedule (v6 API).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Schedule struct {
	ID             int             `json:"id,omitzero"`
	LoadTestID     int             `json:"load_test_id,omitzero"`
	Starts         string          `json:"starts,omitzero"`
	RecurrenceRule *RecurrenceRule `json:"recurrence_rule,omitzero"`
	Deactivated    bool            `json:"deactivated,omitzero"`
	Created        string          `json:"created,omitzero"`
	NextRun        string          `json:"next_run,omitzero"`
}

// RecurrenceRule defines how often a schedule runs.
type RecurrenceRule struct {
	Frequency string `json:"frequency"` // HOURLY, DAILY, WEEKLY, MONTHLY
	Interval  int    `json:"interval"`
}

// ScheduleRequest is the request body for creating or updating a schedule.
type ScheduleRequest struct {
	Starts         string          `json:"starts,omitzero"`
	RecurrenceRule *RecurrenceRule `json:"recurrence_rule,omitzero"`
}

// schedulesResponse is the response from listing schedules.
type schedulesResponse struct {
	Value []Schedule `json:"value"`
	Count int        `json:"@count,omitempty"`
}

// LoadZone represents a K6 private load zone.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type LoadZone struct {
	ID           int    `json:"id,omitzero"`
	Name         string `json:"name"`
	K6LoadZoneID string `json:"k6_load_zone_id,omitzero"`
}

// loadZonesResponse is the response from listing load zones.
type loadZonesResponse struct {
	Value []LoadZone `json:"value"`
}

// PLZCreateRequest is the request body for registering a Private Load Zone.
type PLZCreateRequest struct {
	ProviderID   string      `json:"provider_id,omitzero"`
	K6LoadZoneID string      `json:"k6_load_zone_id,omitzero"`
	PodTiers     PLZPodTiers `json:"pod_tiers,omitzero"`
	Config       PLZConfig   `json:"config,omitzero"`
}

// PLZCreateResponse is the response from registering a Private Load Zone.
type PLZCreateResponse struct {
	Name         string `json:"name,omitzero"`
	K6LoadZoneID string `json:"k6_load_zone_id,omitzero"`
}

// PLZPodTiers defines resource limits for a Private Load Zone.
type PLZPodTiers struct {
	CPU    string `json:"cpu,omitzero"`
	Memory string `json:"memory,omitzero"`
}

// PLZConfig holds optional configuration for a Private Load Zone.
type PLZConfig struct {
	LoadRunnerImage string `json:"load_runner_image,omitzero"`
}

// AllowedProject represents a project allowed to use a load zone.
type AllowedProject struct {
	ID   int    `json:"id,omitzero"`
	Name string `json:"name,omitzero"`
}

// allowedProjectsResponse is the response from listing allowed projects.
type allowedProjectsResponse struct {
	Value []AllowedProject `json:"value"`
}

// AllowedLoadZone represents a load zone allowed for a project.
type AllowedLoadZone struct {
	ID   int    `json:"id,omitzero"`
	Name string `json:"name,omitzero"`
}

// allowedLoadZonesResponse is the response from listing allowed load zones.
type allowedLoadZonesResponse struct {
	Value []AllowedLoadZone `json:"value"`
}
