package investigations

import "time"

// InvestigationSummary is a list item from GET /investigations/summary.
type InvestigationSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
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
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

// CancelResponse is the response from POST /investigations/{id}/cancel.
type CancelResponse struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

// Todo is a single agent task from GET /investigations/{id}/todos.
type Todo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee,omitempty"`
}

// TimelineEntry is a single entry from GET /investigations/{id}/timeline-snapshot.
type TimelineEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
	Actor     string    `json:"actor,omitempty"`
}

// ReportSummary is from GET /investigations/{id}/report-summary.
// Decoded as map[string]any because the report schema may evolve.
type ReportSummary map[string]any

// Document is from GET /investigations/{id}/documents/{docId}.
type Document struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
	Type    string `json:"type,omitempty"`
}

// Approval is from GET /investigations/{id}/approvals.
type Approval struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Approver  string    `json:"approver,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
