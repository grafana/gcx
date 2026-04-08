package eval

import (
	"time"
)

//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type EvaluatorDefinition struct {
	// User-provided fields (spec)
	EvaluatorID string         `json:"evaluator_id"`
	Version     string         `json:"version"`
	Kind        string         `json:"kind"` // llm_judge, json_schema, regex, heuristic
	Description string         `json:"description,omitempty"`
	Config      map[string]any `json:"config"`
	OutputKeys  []OutputKey    `json:"output_keys,omitempty"`

	// Server-generated fields (stripped on push)
	TenantID              string     `json:"tenant_id,omitempty"`
	IsPredefined          bool       `json:"is_predefined,omitempty"`
	SourceTemplateID      string     `json:"source_template_id,omitempty"`
	SourceTemplateVersion string     `json:"source_template_version,omitempty"`
	CreatedBy             string     `json:"created_by,omitempty"`
	UpdatedBy             string     `json:"updated_by,omitempty"`
	DeletedAt             *time.Time `json:"deleted_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at,omitzero"`
	UpdatedAt             time.Time  `json:"updated_at,omitzero"`
}

// GetResourceName implements adapter.ResourceNamer.
func (e EvaluatorDefinition) GetResourceName() string { return e.EvaluatorID }

// SetResourceName implements adapter.ResourceIdentity.
func (e *EvaluatorDefinition) SetResourceName(name string) { e.EvaluatorID = name }

// OutputKey describes one output key of an evaluator.
type OutputKey struct {
	Key           string   `json:"key"`
	Type          string   `json:"type"`
	Description   string   `json:"description,omitempty"`
	Unit          string   `json:"unit,omitempty"`
	PassThreshold *float64 `json:"pass_threshold,omitempty"`
	Enum          []string `json:"enum,omitempty"`
	Min           *float64 `json:"min,omitempty"`
	Max           *float64 `json:"max,omitempty"`
	PassMatch     []string `json:"pass_match,omitempty"`
	PassValue     *bool    `json:"pass_value,omitempty"`
}

//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type RuleDefinition struct {
	// User-provided fields (spec)
	RuleID        string         `json:"rule_id"`
	Enabled       bool           `json:"enabled"`
	Selector      string         `json:"selector"` // user_visible_turn, all_assistant_generations, etc.
	Match         map[string]any `json:"match,omitempty"`
	SampleRate    float64        `json:"sample_rate"`
	EvaluatorIDs  []string       `json:"evaluator_ids"`
	AlertRuleUIDs []string       `json:"alert_rule_uids,omitempty"`

	// Server-generated fields (stripped on push)
	TenantID  string     `json:"tenant_id,omitempty"`
	CreatedBy string     `json:"created_by,omitempty"`
	UpdatedBy string     `json:"updated_by,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at,omitzero"`
	UpdatedAt time.Time  `json:"updated_at,omitzero"`
}

// GetResourceName implements adapter.ResourceNamer.
func (r RuleDefinition) GetResourceName() string { return r.RuleID }

// SetResourceName implements adapter.ResourceIdentity.
func (r *RuleDefinition) SetResourceName(name string) { r.RuleID = name }

// TemplateDefinition is a list item from GET /eval/templates.
type TemplateDefinition struct {
	TemplateID    string     `json:"template_id"`
	Scope         string     `json:"scope"` // global, tenant
	Kind          string     `json:"kind"`
	LatestVersion string     `json:"latest_version,omitempty"`
	Description   string     `json:"description,omitempty"`
	TenantID      string     `json:"tenant_id,omitempty"`
	CreatedBy     string     `json:"created_by,omitempty"`
	UpdatedBy     string     `json:"updated_by,omitempty"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at,omitzero"`
	UpdatedAt     time.Time  `json:"updated_at,omitzero"`
}

// TemplateDetail is the full response from GET /eval/templates/{id}.
// Uses map[string]any because it includes nested config, output_keys, and versions.
type TemplateDetail map[string]any

// TemplateVersion is a version item from GET /eval/templates/{id}/versions.
type TemplateVersion struct {
	TemplateID string         `json:"template_id"`
	Version    string         `json:"version"`
	Config     map[string]any `json:"config,omitempty"`
	OutputKeys []OutputKey    `json:"output_keys,omitempty"`
	Changelog  string         `json:"changelog,omitempty"`
	CreatedBy  string         `json:"created_by,omitempty"`
	UpdatedBy  string         `json:"updated_by,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitzero"`
	UpdatedAt  time.Time      `json:"updated_at,omitzero"`
}

// EvalTestRequest is the request body for POST /eval:test.
type EvalTestRequest struct {
	Kind           string         `json:"kind"`
	Config         map[string]any `json:"config"`
	OutputKeys     []OutputKey    `json:"output_keys"`
	GenerationID   string         `json:"generation_id,omitempty"`
	GenerationData any            `json:"generation_data,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	From           *time.Time     `json:"from,omitempty"`
	To             *time.Time     `json:"to,omitempty"`
	At             *time.Time     `json:"at,omitempty"`
}

// EvalTestResponse is the response from POST /eval:test.
type EvalTestResponse struct {
	GenerationID    string          `json:"generation_id"`
	ConversationID  string          `json:"conversation_id"`
	Scores          []EvalTestScore `json:"scores"`
	ExecutionTimeMs int64           `json:"execution_time_ms"`
}

// EvalTestScore is a single score returned by eval:test.
type EvalTestScore struct {
	Key         string         `json:"key"`
	Type        string         `json:"type"`
	Value       any            `json:"value"`
	Passed      *bool          `json:"passed,omitempty"`
	Explanation string         `json:"explanation,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// JudgeProvider is a provider from GET /eval/judge/providers.
type JudgeProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// JudgeModel is a model from GET /eval/judge/models.
type JudgeModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	ContextWindow int    `json:"context_window"`
}
