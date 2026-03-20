package k6

// Project represents a K6 Cloud project (v6 API).
type Project struct {
	ID               int    `json:"id,omitempty"`
	Name             string `json:"name"`
	IsDefault        bool   `json:"is_default,omitempty"`
	GrafanaFolderUID string `json:"grafana_folder_uid,omitempty"`
	Created          string `json:"created,omitempty"`
	Updated          string `json:"updated,omitempty"`
}

// LoadTest represents a K6 load test (v6 API).
type LoadTest struct {
	ID        int    `json:"id,omitempty"`
	Name      string `json:"name"`
	ProjectID int    `json:"project_id"`
	Script    string `json:"script,omitempty"`
	Created   string `json:"created,omitempty"`
	Updated   string `json:"updated,omitempty"`
}

// EnvVar represents a K6 Cloud environment variable.
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
