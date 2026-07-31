package investigations

import "time"

// --- v2 (Lodestone) types ---
//
// Lodestone is the single-agent successor to the legacy investigations
// engine. The two APIs share a plugin base path but have distinct
// request/response shapes, so v2 types live in their own file.

// CreateLodestoneRequest is the body for POST /investigations/lodestone.
type CreateLodestoneRequest struct {
	Instruction    string   `json:"instruction"`
	Title          string   `json:"title,omitempty"`
	TeamNames      []string `json:"teamNames,omitempty"`
	AgentProfileID string   `json:"agentProfileId,omitempty"`
}

// CreateLodestoneResponse is the response from POST /investigations/lodestone.
type CreateLodestoneResponse struct {
	InvestigationID string `json:"investigationId"`
	ChatID          string `json:"chatId"`
	AgentProfileID  string `json:"agentProfileId,omitempty"`
}

// ListLodestoneOptions holds the optional filters for
// GET /investigations/lodestone.
type ListLodestoneOptions struct {
	State         string
	Q             string
	Scope         string
	TeamName      string
	From          string
	To            string
	Sort          string
	Order         string
	View          string
	Limit         int
	Offset        int
	Label         string
	IncludeLegacy bool
}

// LodestoneList is the collection envelope from GET /api/v2/investigations.
// Total is the number of matching investigations across all pages, for
// offset-based pagination.
type LodestoneList struct {
	Investigations []LodestoneInvestigationSummary `json:"investigations"`
	Total          int64                           `json:"total"`
}

// ListItemsKey satisfies internal/output's ListEnvelope so that --json field
// selection and discovery descend into the investigations rather than
// operating on the envelope keys.
func (LodestoneList) ListItemsKey() string { return "investigations" }

// LodestoneInvestigationSummary is a v2 list item. It mirrors the server's
// summary shape field-for-field — including which fields carry omitempty —
// so no data is dropped or reshaped in json/yaml output. The deprecated
// `confidence` field (always null server-side) is omitted.
type LodestoneInvestigationSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	ChatID      string    `json:"chatId"`
	Variant     string    `json:"variant,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// TokensUsed is the summed input+output+cache token count of the backing
	// chat. Only populated for view=full.
	TokensUsed        *int                   `json:"tokensUsed,omitempty"`
	Source            *LodestoneSource       `json:"source,omitempty"`
	Agents            []LodestoneAgent       `json:"agents,omitempty"`
	Progress          *LodestoneTodoProgress `json:"progress,omitempty"`
	CompletionQuality *string                `json:"completionQuality,omitempty"`
	DegradedReason    *string                `json:"degradedReason,omitempty"`
	Labels            map[string]string      `json:"labels,omitempty"`
	OwnerUserID       string                 `json:"ownerUserId,omitempty"`
	ActiveLoopCount   int                    `json:"activeLoopCount,omitempty"`
}

// LodestoneSource identifies what created an investigation
// (type: url|user|assistant).
type LodestoneSource struct {
	Type   string `json:"type"`
	Value  string `json:"value,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	ChatID string `json:"chatId,omitempty"`
	UserID string `json:"userId,omitempty"`
}

// LodestoneAgent is per-agent progress data attached to a summary
// (view=full only).
type LodestoneAgent struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Task                   string    `json:"task"`
	FinalMessage           *string   `json:"finalMessage,omitempty"`
	Status                 string    `json:"status"`
	Audience               string    `json:"audience"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
	TokensPerSecondHistory []float64 `json:"tokensPerSecondHistory,omitempty"`
	TokenCounter           *int64    `json:"tokenCounter,omitempty"`
	OutputPreview          *string   `json:"outputPreview,omitempty"`
}

// LodestoneTodoProgress is the todo completion breakdown attached to a
// summary (view=full only).
type LodestoneTodoProgress struct {
	Pending    int `json:"pending"`
	InProgress int `json:"inProgress"`
	Completed  int `json:"completed"`
	Canceled   int `json:"canceled"`
	Total      int `json:"total"`
}

// LodestoneState is the response from GET /investigations/lodestone/{chatId}/state.
// Decoded as map[string]any because the session shape is rich and may evolve.
type LodestoneState map[string]any

// ResolveByIDResponse is the response from
// GET /investigations/lodestone/by-id/{investigationId}.
type ResolveByIDResponse struct {
	InvestigationID string `json:"investigationId"`
	ChatID          string `json:"chatId"`
}

// Message is a shared reply shape for pause/resume.
type Message struct {
	Message string `json:"message,omitempty"`
}

// ModeRequest is the body for PUT /investigations/lodestone/{chatId}/mode.
type ModeRequest struct {
	Mode string `json:"mode"`
}

// ModeResponse is the response from PUT .../mode.
type ModeResponse struct {
	Message string `json:"message,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// ScopeRequest is the body for POST /investigations/lodestone/{investigationId}/scope.
type ScopeRequest struct {
	TeamNames []string `json:"teamNames"`
}

// ScopeResponse is the response from POST .../scope.
type ScopeResponse struct {
	InvestigationID string   `json:"investigationId"`
	TeamNames       []string `json:"teamNames,omitempty"`
	AddedTeamNames  []string `json:"addedTeamNames,omitempty"`
}
