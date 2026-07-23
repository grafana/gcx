package investigations

import "time"

// InvestigationSummary is a list item from GET /investigations/summary.
type InvestigationSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Source    *Source   `json:"source,omitempty"`
}

// Source identifies who created an investigation.
type Source struct {
	Type   string `json:"type,omitempty"`
	UserID string `json:"userId,omitempty"`
}

// Investigation is the full detail from GET /investigations/{id}.
// Decoded as map[string]any because the response may contain complex nested
// objects whose schema can evolve independently of this CLI.
type Investigation map[string]any

// CreateRequest is the body for POST /investigations.
type CreateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// CreateResponse is the response from POST /investigations.
type CreateResponse struct {
	ID    string `json:"id"`
	State string `json:"state,omitempty"`
}

// CancelResponse is the response from POST /investigations/{id}/cancel.
type CancelResponse struct {
	Message string `json:"message,omitempty"`
}
