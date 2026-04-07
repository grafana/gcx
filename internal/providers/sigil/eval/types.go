package eval

import "time"

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
