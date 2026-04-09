package scores

import (
	"fmt"
	"strconv"
	"time"
)

// Score is a single evaluation score for a generation.
type Score struct {
	ScoreID          string         `json:"score_id"`
	GenerationID     string         `json:"generation_id"`
	ConversationID   string         `json:"conversation_id,omitempty"`
	EvaluatorID      string         `json:"evaluator_id"`
	EvaluatorVersion string         `json:"evaluator_version"`
	RuleID           string         `json:"rule_id,omitempty"`
	RunID            string         `json:"run_id,omitempty"`
	ScoreKey         string         `json:"score_key"`
	ScoreType        string         `json:"score_type"` // number, bool, string
	Value            ScoreValue     `json:"value"`
	Unit             string         `json:"unit,omitempty"`
	Passed           *bool          `json:"passed,omitempty"`
	Explanation      string         `json:"explanation,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	TraceID          string         `json:"trace_id,omitempty"`
	SpanID           string         `json:"span_id,omitempty"`
	Source           *ScoreSource   `json:"source,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

// ScoreValue is a union type for score values (number, bool, or string).
type ScoreValue struct {
	Number *float64 `json:"number,omitempty"`
	Bool   *bool    `json:"bool,omitempty"`
	String *string  `json:"string,omitempty"`
}

// Display returns a human-readable representation of the score value.
func (v ScoreValue) Display() string {
	switch {
	case v.Number != nil:
		return fmt.Sprintf("%g", *v.Number)
	case v.Bool != nil:
		return strconv.FormatBool(*v.Bool)
	case v.String != nil:
		return *v.String
	default:
		return "-"
	}
}

// ScoreSource identifies where the score came from.
type ScoreSource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
