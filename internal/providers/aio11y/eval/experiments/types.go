package experiments

import (
	"time"

	"github.com/grafana/gcx/internal/providers/aio11y/scores"
)

// Experiment is a single eval experiment run.
type Experiment struct {
	// User-provided fields (spec).
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	Tags         []string              `json:"tags,omitempty"`
	Source       string                `json:"source,omitempty"`
	CollectionID string                `json:"collection_id,omitempty"`
	Evaluators   []ExperimentEvaluator `json:"evaluators,omitempty"`
	Metadata     map[string]any        `json:"metadata,omitempty"`

	// Server-managed fields.
	RunID       string     `json:"run_id,omitempty"`
	TenantID    string     `json:"tenant_id,omitempty"`
	Status      string     `json:"status,omitempty"`
	ScoreCount  int        `json:"score_count,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedBy   string     `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at,omitzero"`
	UpdatedAt   time.Time  `json:"updated_at,omitzero"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
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
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
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
	Run        Experiment                 `json:"run"`
	Summary    ExperimentReportSummary    `json:"summary"`
	Breakdowns ExperimentReportBreakdowns `json:"breakdowns"`
	Points     []ExperimentReportPoint    `json:"points"`
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
