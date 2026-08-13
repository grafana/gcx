package experiments

import (
	"time"

	"github.com/grafana/gcx/internal/providers/agento11y/scores"
)

// TestSuite is a versioned collection of test cases.
type TestSuite struct {
	TenantID      string             `json:"tenant_id,omitempty"`
	SuiteID       string             `json:"suite_id,omitempty"`
	Name          string             `json:"name"`
	Description   string             `json:"description,omitempty"`
	Tags          []string           `json:"tags,omitempty"`
	LatestVersion string             `json:"latest_version,omitempty"`
	Versions      []TestSuiteVersion `json:"versions,omitempty"`
	CreatedBy     string             `json:"created_by,omitempty"`
	UpdatedBy     string             `json:"updated_by,omitempty"`
	CreatedAt     time.Time          `json:"created_at,omitzero"`
	UpdatedAt     time.Time          `json:"updated_at,omitzero"`
}

// TestSuiteVersion is one immutable/publishable version of a test suite.
type TestSuiteVersion struct {
	TenantID      string     `json:"tenant_id,omitempty"`
	SuiteID       string     `json:"suite_id"`
	Version       string     `json:"version"`
	TestCaseCount int        `json:"test_case_count"`
	Changelog     string     `json:"changelog,omitempty"`
	Published     bool       `json:"published"`
	SourceVersion string     `json:"source_version,omitempty"`
	CreatedBy     string     `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at,omitzero"`
	PublishedBy   string     `json:"published_by,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
}

// TestCase is a single input/expected record within a suite version.
type TestCase struct {
	TenantID     string         `json:"tenant_id,omitempty"`
	SuiteID      string         `json:"suite_id,omitempty"`
	SuiteVersion string         `json:"suite_version,omitempty"`
	TestCaseID   string         `json:"test_case_id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Description  string         `json:"description,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Category     string         `json:"category,omitempty"`
	Input        map[string]any `json:"input,omitempty"`
	Expected     map[string]any `json:"expected,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	ArtifactRefs []ArtifactRef  `json:"artifact_refs,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitzero"`
	UpdatedAt    time.Time      `json:"updated_at,omitzero"`
}

type ArtifactRef struct {
	ArtifactID string `json:"artifact_id"`
	Name       string `json:"name,omitempty"`
	Kind       string `json:"kind"`
}

type Artifact struct {
	TenantID   string         `json:"tenant_id,omitempty"`
	ArtifactID string         `json:"artifact_id"`
	ParentKind string         `json:"parent_kind"`
	ParentID   string         `json:"parent_id"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Mime       string         `json:"mime,omitempty"`
	ContentRef string         `json:"content_ref,omitempty"`
	SizeBytes  int64          `json:"size_bytes,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitzero"`
	CreatedBy  string         `json:"created_by,omitempty"`
}

// Experiment is a single eval experiment run.
//
// The same struct is the body of experiments create. That endpoint rejects
// unknown fields, and of the server-managed fields it accepts only
// experiment_id, created_by, and planned_trial_count, so a create body
// carrying any of the others fails with 400.
type Experiment struct {
	// User-provided fields (spec).
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Tags         []string             `json:"tags,omitempty"`
	SuiteID      string               `json:"suite_id,omitempty"`
	SuiteVersion string               `json:"suite_version,omitempty"`
	Candidate    *ExperimentCandidate `json:"candidate,omitempty"`
	Metadata     map[string]any       `json:"metadata,omitempty"`

	// Server-managed fields.
	ExperimentID string     `json:"experiment_id,omitempty"`
	TenantID     string     `json:"tenant_id,omitempty"`
	Status       string     `json:"status,omitempty"`
	Error        string     `json:"error,omitempty"`
	CreatedBy    string     `json:"created_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at,omitzero"`
	UpdatedAt    time.Time  `json:"updated_at,omitzero"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`

	// PlannedTrialCount is supplied by the runner at creation time. Nil means
	// unknown; zero is a known empty plan. Result is the finalized rollup, or a
	// progress snapshot while the experiment runs.
	PlannedTrialCount *int                     `json:"planned_trial_count,omitempty"`
	ResultStatus      string                   `json:"result_status,omitempty"`
	ResultError       string                   `json:"result_error,omitempty"`
	Result            *ExperimentReportSummary `json:"result,omitempty"`
}

func (e Experiment) ID() string {
	return e.ExperimentID
}

// ExperimentCandidate identifies what was evaluated.
type ExperimentCandidate struct {
	AgentName     string `json:"agent_name,omitempty"`
	AgentVersion  string `json:"agent_version,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	ModelName     string `json:"model_name,omitempty"`
	GitSHA        string `json:"git_sha,omitempty"`
}

// UpdateRequest is the partial-PATCH body for the update endpoint. Pointer
// fields let callers send only the fields they want to change.
//
// Only user-editable fields are exposed. Status and error are
// server-managed lifecycle fields — clients drive status transitions
// via Cancel, and the server owns the error message. Metadata is not
// patchable through the CLI yet; add a field here when wiring it up.
type UpdateRequest struct {
	Name         *string              `json:"name,omitempty"`
	Description  *string              `json:"description,omitempty"`
	Tags         *[]string            `json:"tags,omitempty"`
	SuiteID      *string              `json:"suite_id,omitempty"`
	SuiteVersion *string              `json:"suite_version,omitempty"`
	Candidate    *ExperimentCandidate `json:"candidate,omitempty"`
	Metadata     map[string]any       `json:"metadata,omitempty"`
}

// ScoreItem is one score record produced by an evaluator during an experiment.
// The field set matches what the experiments scores endpoint returns.
//
// It is separate from scores.Score because the experiments scores endpoint
// returns more fields (tenant, evaluator description, ingestion time,
// agent/version metadata) and emits a flat source_kind/source_id pair rather
// than the nested {source: {kind, id}} envelope scores.Score uses.
type ScoreItem struct {
	TenantID             string         `json:"tenant_id"`
	ScoreID              string         `json:"score_id"`
	GenerationID         string         `json:"generation_id,omitempty"`
	ConversationID       string         `json:"conversation_id,omitempty"`
	TraceID              string         `json:"trace_id,omitempty"`
	SpanID               string         `json:"span_id,omitempty"`
	TrialID              string         `json:"trial_id,omitempty"`
	TestCaseID           string         `json:"test_case_id,omitempty"`
	GraderConversationID string         `json:"grader_conversation_id,omitempty"`
	GraderGenerationID   string         `json:"grader_generation_id,omitempty"`
	GraderTraceID        string         `json:"grader_trace_id,omitempty"`
	EvaluatorID          string         `json:"evaluator_id"`
	EvaluatorVersion     string         `json:"evaluator_version"`
	EvaluatorDescription string         `json:"evaluator_description,omitempty"`
	EvaluatorRole        string         `json:"evaluator_role,omitempty"`
	RuleID               string         `json:"rule_id,omitempty"`
	ExperimentID         string         `json:"experiment_id,omitempty"`
	ScoreKey             string         `json:"score_key"`
	ScoreType            string         `json:"score_type"`
	Value                ScoreValue     `json:"value"`
	Unit                 string         `json:"unit,omitempty"`
	Passed               *bool          `json:"passed,omitempty"`
	Explanation          string         `json:"explanation,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	IngestedAt           time.Time      `json:"ingested_at"`
	SourceKind           string         `json:"source_kind,omitempty"`
	SourceID             string         `json:"source_id,omitempty"`
	AgentName            string         `json:"agent_name,omitempty"`
	EffectiveVersion     string         `json:"effective_version,omitempty"`
}

// ScoreValue is the polymorphic value of a score (numeric, boolean, or string).
type ScoreValue = scores.ScoreValue

// ExperimentReport summarises the outcome of an experiment. The field set
// matches the report endpoint's response.
type ExperimentReport struct {
	Experiment Experiment              `json:"experiment"`
	Summary    ExperimentReportSummary `json:"summary"`
	Rows       []TestCaseResultRow     `json:"rows"`
}

// ExperimentReportSummary holds aggregate counts for an experiment. The field
// set matches the summary object in the report endpoint's response.
//
// The API omits an aggregate it did not measure, so those fields are pointers:
// nil means unmeasured, and zero means measured as zero. TokenCoverage and
// CostCoverage ("none", "partial", or "complete") say whether a total that is
// present covers every trial.
type ExperimentReportSummary struct {
	TestCaseCount   int                `json:"test_case_count"`
	TrialCount      int                `json:"trial_count"`
	CompletedCount  int                `json:"completed_count"`
	FailedCount     int                `json:"failed_count"`
	CanceledCount   int                `json:"canceled_count"`
	PassRate        *float64           `json:"pass_rate,omitempty"`
	PassAtK         map[string]float64 `json:"pass_at_k,omitempty"`
	PassPowerK      map[string]float64 `json:"pass_power_k,omitempty"`
	FinalScoreAvg   *float64           `json:"final_score_avg,omitempty"`
	TotalCost       *float64           `json:"total_cost,omitempty"`
	TotalTokens     *int64             `json:"total_tokens,omitempty"`
	PassCount       int                `json:"pass_count"`
	PassDenominator int                `json:"pass_denominator"`
	FinalScoreSum   float64            `json:"final_score_sum"`
	FinalScoreCount int                `json:"final_score_count"`
	TokenCoverage   string             `json:"token_coverage"`
	CostCoverage    string             `json:"cost_coverage"`
}

type TestCaseSnapshot struct {
	TestCaseID   string         `json:"test_case_id"`
	SuiteID      string         `json:"suite_id,omitempty"`
	SuiteVersion string         `json:"suite_version,omitempty"`
	Name         string         `json:"name,omitempty"`
	Description  string         `json:"description,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Category     string         `json:"category,omitempty"`
	Input        map[string]any `json:"input,omitempty"`
	Expected     map[string]any `json:"expected,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	ArtifactRefs []ArtifactRef  `json:"artifact_refs,omitempty"`
}

// TestCaseTrial is one attempt at one test case.
//
// The same struct is the body of trials create. That endpoint rejects unknown
// fields, and accepts none of tenant_id, total_tokens, created_at, or
// updated_at, so a create body holding any of those fails with 400. The API
// derives total_tokens from the trial's conversation usage.
type TestCaseTrial struct {
	TenantID       string            `json:"tenant_id,omitempty"`
	TrialID        string            `json:"trial_id,omitempty"`
	ExperimentID   string            `json:"experiment_id,omitempty"`
	TestCaseID     string            `json:"test_case_id"`
	TestCase       *TestCaseSnapshot `json:"test_case,omitempty"`
	Attempt        int               `json:"attempt"`
	Status         string            `json:"status,omitempty"`
	TraceID        string            `json:"trace_id,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
	Cost           *float64          `json:"cost,omitempty"`
	InputTokens    *int64            `json:"input_tokens,omitempty"`
	OutputTokens   *int64            `json:"output_tokens,omitempty"`
	TotalTokens    *int64            `json:"total_tokens,omitempty"`
	DurationMS     *int64            `json:"duration_ms,omitempty"`
	Error          string            `json:"error,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	StartedAt      *time.Time        `json:"started_at,omitempty"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at,omitzero"`
	UpdatedAt      time.Time         `json:"updated_at,omitzero"`
}

type UpdateTrialRequest struct {
	Status         *string        `json:"status,omitempty"`
	TraceID        *string        `json:"trace_id,omitempty"`
	SpanID         *string        `json:"span_id,omitempty"`
	ConversationID *string        `json:"conversation_id,omitempty"`
	Cost           *float64       `json:"cost,omitempty"`
	InputTokens    *int64         `json:"input_tokens,omitempty"`
	OutputTokens   *int64         `json:"output_tokens,omitempty"`
	DurationMS     *int64         `json:"duration_ms,omitempty"`
	Error          *string        `json:"error,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
}

type TestCaseResultRow struct {
	TestCaseID       string                   `json:"test_case_id"`
	TestCaseSnapshot *TestCaseSnapshot        `json:"test_case_snapshot,omitempty"`
	Summary          TestCaseResultRowSummary `json:"summary"`
	Trials           []TestCaseTrialResult    `json:"trials"`
}

type TestCaseResultRowSummary struct {
	TrialCount     int             `json:"trial_count"`
	CompletedCount int             `json:"completed_count"`
	PassAtK        map[string]bool `json:"pass_at_k,omitempty"`
	PassPowerK     map[string]bool `json:"pass_power_k,omitempty"`
	TrialPassRate  *float64        `json:"trial_pass_rate,omitempty"`
}

type TestCaseTrialResult struct {
	Trial      TestCaseTrial `json:"trial"`
	FinalScore *ScoreItem    `json:"final_score,omitempty"`
	Scores     []ScoreItem   `json:"scores"`
	Artifacts  []Artifact    `json:"artifacts"`
}
