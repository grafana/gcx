package experiments

import (
	"time"

	"github.com/grafana/gcx/internal/providers/aio11y/scores"
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
type Experiment struct {
	// User-provided fields (spec).
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	Tags         []string              `json:"tags,omitempty"`
	SuiteID      string                `json:"suite_id,omitempty"`
	SuiteVersion string                `json:"suite_version,omitempty"`
	Candidate    *ExperimentCandidate  `json:"candidate,omitempty"`
	Source       string                `json:"source,omitempty"`
	CollectionID string                `json:"collection_id,omitempty"`
	Evaluators   []ExperimentEvaluator `json:"evaluators,omitempty"`
	Metadata     map[string]any        `json:"metadata,omitempty"`

	// Server-managed fields.
	ExperimentID string     `json:"experiment_id,omitempty"`
	RunID        string     `json:"run_id,omitempty"`
	TenantID     string     `json:"tenant_id,omitempty"`
	Status       string     `json:"status,omitempty"`
	ScoreCount   int        `json:"score_count,omitempty"`
	Error        string     `json:"error,omitempty"`
	CreatedBy    string     `json:"created_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at,omitzero"`
	UpdatedAt    time.Time  `json:"updated_at,omitzero"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

func (e Experiment) ID() string {
	if e.ExperimentID != "" {
		return e.ExperimentID
	}
	return e.RunID
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

// ExperimentEvaluator binds an evaluator to the experiment, optionally with a
// selector that scopes which scored items it applies to.
type ExperimentEvaluator struct {
	ID       string `json:"id"`
	Selector string `json:"selector,omitempty"`
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
//
// This is intentionally separate from scores.Score: the experiments scores
// endpoint returns more fields (tenant, evaluator description, ingestion
// time, agent/version metadata) and emits a flat source_kind/source_id pair
// rather than the nested {source: {kind, id}} envelope used by scores.Score.
// Keep the two in sync when adding fields that exist on both endpoints.
type ScoreItem struct {
	TenantID             string         `json:"tenant_id"`
	ScoreID              string         `json:"score_id"`
	GenerationID         string         `json:"generation_id,omitempty"`
	ConversationID       string         `json:"conversation_id,omitempty"`
	TraceID              string         `json:"trace_id,omitempty"`
	SpanID               string         `json:"span_id,omitempty"`
	TrialID              string         `json:"trial_id,omitempty"`
	TestCaseID           string         `json:"test_case_id,omitempty"`
	EvaluatorID          string         `json:"evaluator_id"`
	EvaluatorVersion     string         `json:"evaluator_version"`
	EvaluatorDescription string         `json:"evaluator_description,omitempty"`
	RuleID               string         `json:"rule_id,omitempty"`
	RunID                string         `json:"run_id,omitempty"`
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

// ExperimentReport summarises the outcome of an experiment.
type ExperimentReport struct {
	Experiment Experiment                 `json:"experiment"`
	Run        Experiment                 `json:"run,omitzero"`
	Summary    ExperimentReportSummary    `json:"summary"`
	Rows       []TestCaseResultRow        `json:"rows,omitempty"`
	Breakdowns ExperimentReportBreakdowns `json:"breakdowns,omitzero"`
	Points     []ExperimentReportPoint    `json:"points,omitempty"`
}

// ExperimentReportSummary holds aggregate counts for an experiment.
type ExperimentReportSummary struct {
	NConversations int     `json:"n_conversations"`
	NGenerations   int     `json:"n_generations"`
	NScores        int     `json:"n_scores"`
	PassRate       float64 `json:"pass_rate"`
	MeanScore      float64 `json:"mean_score"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	TotalTokens    int64   `json:"total_tokens"`

	TestCaseCount  int                `json:"test_case_count,omitempty"`
	TrialCount     int                `json:"trial_count,omitempty"`
	CompletedCount int                `json:"completed_count,omitempty"`
	FailedCount    int                `json:"failed_count,omitempty"`
	CanceledCount  int                `json:"canceled_count,omitempty"`
	PassAtK        map[string]float64 `json:"pass_at_k,omitempty"`
	PassPowerK     map[string]float64 `json:"pass_power_k,omitempty"`
	FinalScoreAvg  *float64           `json:"final_score_avg,omitempty"`
	TotalCost      *float64           `json:"total_cost,omitempty"`
}

// ExperimentReportBreakdowns holds aggregate breakdowns grouped by dimension.
type ExperimentReportBreakdowns struct {
	ByTask              []ExperimentReportBreakdown `json:"by_task"`
	ByCategory          []ExperimentReportBreakdown `json:"by_category"`
	ByEvaluator         []ExperimentReportBreakdown `json:"by_evaluator"`
	ByScoreKey          []ExperimentReportBreakdown `json:"by_score_key"`
	ByEvaluatorScoreKey []ExperimentReportBreakdown `json:"by_evaluator_score_key"`
}

// ExperimentReportBreakdown holds one aggregate bucket.
type ExperimentReportBreakdown struct {
	Key          string  `json:"key"`
	Count        int     `json:"count"`
	PassRate     float64 `json:"pass_rate"`
	MeanScore    float64 `json:"mean_score"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	TotalTokens  int64   `json:"total_tokens"`
}

// ExperimentReportPoint is one score point included in an experiment report.
type ExperimentReportPoint struct {
	ConversationID   string         `json:"conversation_id"`
	GenerationID     string         `json:"generation_id"`
	ScoreID          string         `json:"score_id"`
	TaskID           string         `json:"task_id,omitempty"`
	TaskCategory     string         `json:"task_category,omitempty"`
	TrialID          string         `json:"trial_id,omitempty"`
	EvaluatorID      string         `json:"evaluator_id"`
	EvaluatorVersion string         `json:"evaluator_version,omitempty"`
	ScoreKey         string         `json:"score_key"`
	ScoreType        string         `json:"score_type"`
	Value            ScoreValue     `json:"value"`
	ValueNumber      *float64       `json:"value_number,omitempty"`
	Passed           *bool          `json:"passed,omitempty"`
	Explanation      string         `json:"explanation,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CostUSD          float64        `json:"cost_usd,omitempty"`
	Tokens           int64          `json:"tokens,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
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
