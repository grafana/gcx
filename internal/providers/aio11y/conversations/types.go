package conversations

import "time"

// Conversation is a list item from the upstream AI Observability query API
// (proxied via GET /query/conversations).
type Conversation struct {
	ID                string         `json:"id"`
	Title             string         `json:"title,omitempty"`
	GenerationCount   int            `json:"generation_count"`
	FirstGenerationAt time.Time      `json:"first_generation_at"`
	LastGenerationAt  time.Time      `json:"last_generation_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	RatingSummary     *RatingSummary `json:"rating_summary,omitempty"`
}

// SearchResult is an enriched result from the plugin's search endpoint
// (POST /query/conversations/search). Has different field names than Conversation.
type SearchResult struct {
	ConversationID    string            `json:"conversation_id"`
	ConversationTitle string            `json:"conversation_title,omitempty"`
	UserID            string            `json:"user_id,omitempty"`
	GenerationCount   int               `json:"generation_count"`
	FirstGenerationAt time.Time         `json:"first_generation_at"`
	LastGenerationAt  time.Time         `json:"last_generation_at"`
	Models            []string          `json:"models"`
	ModelProviders    map[string]string `json:"model_providers,omitempty"`
	Agents            []string          `json:"agents"`
	ErrorCount        int               `json:"error_count"`
	HasErrors         bool              `json:"has_errors"`
	TraceIDs          []string          `json:"trace_ids"`
	RatingSummary     *RatingSummary    `json:"rating_summary,omitempty"`
	AnnotationCount   int               `json:"annotation_count"`
	EvalSummary       *EvalSummary      `json:"eval_summary,omitempty"`
}

// ConversationDetail is the full detail response for a single conversation.
// Decoded as map[string]any because the generations array contains complex
// nested objects (model is an object, input/output vary by provider).
type ConversationDetail map[string]any

// RatingSummary holds conversation rating aggregates.
type RatingSummary struct {
	TotalCount   int    `json:"total_count"`
	GoodCount    int    `json:"good_count"`
	BadCount     int    `json:"bad_count"`
	LatestRating string `json:"latest_rating,omitempty"`
	HasBadRating bool   `json:"has_bad_rating"`
}

// EvalSummary holds evaluation score aggregates.
type EvalSummary struct {
	TotalScores int `json:"total_scores"`
	PassCount   int `json:"pass_count"`
	FailCount   int `json:"fail_count"`
}

// SearchRequest is the request body for POST /query/conversations/search.
type SearchRequest struct {
	Filters   string           `json:"filters,omitempty"`
	TimeRange *SearchTimeRange `json:"time_range,omitempty"`
	PageSize  int              `json:"page_size,omitempty"`
	Cursor    string           `json:"cursor,omitempty"`
}

// SearchTimeRange constrains the search to a time window.
type SearchTimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// SearchResponse is the response from the search endpoint.
type SearchResponse struct {
	Conversations []SearchResult `json:"conversations"`
	NextCursor    string         `json:"next_cursor,omitempty"`
	HasMore       bool           `json:"has_more"`
}
