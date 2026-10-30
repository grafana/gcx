package reviewqueues

import (
	"time"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/humanevaluators"
)

// ScoreValue is a reviewer's answer: exactly one of the fields is set.
type ScoreValue struct {
	Number *float64 `json:"number,omitempty"`
	Bool   *bool    `json:"bool,omitempty"`
	String *string  `json:"string,omitempty"`
}

// Score is one reviewer answer shown in a queue.
type Score struct {
	EvaluatorID      string     `json:"evaluator_id"`
	EvaluatorVersion string     `json:"evaluator_version"`
	ScoreKey         string     `json:"score_key"`
	Value            ScoreValue `json:"value"`
	Passed           *bool      `json:"passed,omitempty"`
	Reviewer         string     `json:"reviewer"`
	Explanation      string     `json:"explanation,omitempty"`
	CreatedAt        time.Time  `json:"created_at,omitzero"`
}

// Item is one conversation in a queue.
type Item struct {
	ConversationID string    `json:"conversation_id"`
	Name           string    `json:"name,omitempty"`
	AgentNames     []string  `json:"agent_names,omitempty"`
	Reviewed       bool      `json:"reviewed"`
	Reviewers      []string  `json:"reviewers"`
	Scores         []Score   `json:"scores"`
	AddedAt        time.Time `json:"added_at,omitzero"`
}

// ReviewQueue is a collection marked for human review. The list endpoint
// omits Items; Get includes them.
type ReviewQueue struct {
	CollectionID         string                       `json:"collection_id"`
	Name                 string                       `json:"name"`
	Rubric               []humanevaluators.RubricItem `json:"rubric"`
	Total                int                          `json:"total"`
	Reviewed             int                          `json:"reviewed"`
	Pending              int                          `json:"pending"`
	NextConversationID   string                       `json:"next_conversation_id,omitempty"`
	NextConversationName string                       `json:"next_conversation_name,omitempty"`
	Truncated            bool                         `json:"truncated,omitempty"`
	Items                []Item                       `json:"items,omitempty"`
}

// ConversationView is the queue as seen from one conversation: the rubric,
// the queue's progress, and that conversation's membership and answers.
type ConversationView struct {
	CollectionID         string                       `json:"collection_id"`
	Name                 string                       `json:"name"`
	Rubric               []humanevaluators.RubricItem `json:"rubric"`
	Total                int                          `json:"total"`
	Reviewed             int                          `json:"reviewed"`
	Truncated            bool                         `json:"truncated,omitempty"`
	NextConversationID   string                       `json:"next_conversation_id,omitempty"`
	NextConversationName string                       `json:"next_conversation_name,omitempty"`
	Member               bool                         `json:"member"`
	Position             int                          `json:"position,omitempty"`
	Item                 Item                         `json:"item"`
}

// RubricRequest is the body of PUT /eval/review-queues/{collection_id}.
type RubricRequest struct {
	RubricEvaluatorIDs []string `json:"rubric_evaluator_ids"`
}

// Answer is one answer to one rubric question. EvaluatorVersion is the
// question version the reviewer saw; the backend rejects an answer to a
// version that is no longer current.
type Answer struct {
	EvaluatorID      string     `json:"evaluator_id"`
	EvaluatorVersion string     `json:"evaluator_version"`
	Value            ScoreValue `json:"value"`
}

// ReviewRequest is one reviewer's answers for one conversation in a queue.
type ReviewRequest struct {
	ConversationID string   `json:"conversation_id"`
	GenerationID   string   `json:"generation_id"`
	Answers        []Answer `json:"answers"`
	Note           string   `json:"note,omitempty"`
}

// ReviewResult is the response to a review submission: the stored scores,
// returned as the backend sent them.
type ReviewResult struct {
	Scores []map[string]any `json:"scores"`
}

// ScoreRow is one answer in a queue's CSV export. Every cell is the export's
// text: the backend prefixes text cells that start with a formula character
// (= + - @) with an apostrophe.
type ScoreRow struct {
	ConversationID   string `json:"conversation_id"`
	GenerationID     string `json:"generation_id"`
	AgentName        string `json:"agent_name"`
	AgentVersion     string `json:"agent_version"`
	EvaluatorID      string `json:"evaluator_id"`
	EvaluatorVersion string `json:"evaluator_version"`
	ScoreKey         string `json:"score_key"`
	Value            string `json:"value"`
	Passed           string `json:"passed"`
	Reviewer         string `json:"reviewer"`
	Note             string `json:"note"`
	CreatedAt        string `json:"created_at"`
}
