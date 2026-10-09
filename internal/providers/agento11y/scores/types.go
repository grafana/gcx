package scores

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Score is a single evaluation score for a generation or rule.
type Score struct {
	ScoreID           string         `json:"score_id"`
	GenerationID      string         `json:"generation_id"`
	ConversationID    string         `json:"conversation_id,omitempty"`
	ConversationTitle string         `json:"conversation_title,omitempty"`
	EvaluatorID       string         `json:"evaluator_id"`
	EvaluatorVersion  string         `json:"evaluator_version"`
	RuleID            string         `json:"rule_id,omitempty"`
	RunID             string         `json:"run_id,omitempty"`
	ScoreKey          string         `json:"score_key"`
	ScoreType         string         `json:"score_type"` // number, bool, string
	Value             ScoreValue     `json:"value"`
	Unit              string         `json:"unit,omitempty"`
	Passed            *bool          `json:"passed,omitempty"`
	Explanation       string         `json:"explanation,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	TraceID           string         `json:"trace_id,omitempty"`
	SpanID            string         `json:"span_id,omitempty"`
	// Source is the provenance. The generation endpoint
	// (/query/generations/<id>/scores) returns a nested {source: {kind, id}}
	// envelope; the eval endpoints (/eval/rules/<id>/scores, experiments) return
	// a flat source_kind/source_id pair. SourceKind/SourceID capture the flat
	// wire form on decode; UnmarshalJSON folds them into Source so callers see a
	// single shape regardless of which endpoint produced the row.
	Source      *ScoreSource `json:"source,omitempty"`
	SourceKind  string       `json:"source_kind,omitempty"`
	SourceID    string       `json:"source_id,omitempty"`
	AgentName   string       `json:"agent_name,omitempty"`
	GenModel    string       `json:"gen_model,omitempty"`
	GenProvider string       `json:"gen_provider,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
}

// UnmarshalJSON decodes a score and normalizes provenance to the nested Source
// form: when the wire carries the eval endpoints' flat source_kind/source_id
// pair and no nested source, they are folded into Source and the flat fields
// cleared, so re-marshaling (e.g. -o json) emits one consistent shape.
func (s *Score) UnmarshalJSON(data []byte) error {
	type alias Score
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = Score(a)
	if s.Source == nil && (s.SourceKind != "" || s.SourceID != "") {
		s.Source = &ScoreSource{Kind: s.SourceKind, ID: s.SourceID}
		s.SourceKind = ""
		s.SourceID = ""
	}
	return nil
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
