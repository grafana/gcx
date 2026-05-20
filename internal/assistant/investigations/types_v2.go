package investigations

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

// LodestoneState is the response from GET /investigations/lodestone/{chatId}/state.
// Decoded as map[string]any because the session shape is rich and may evolve.
type LodestoneState map[string]any

// ResolveByIDResponse is the response from
// GET /investigations/lodestone/by-id/{investigationId}.
type ResolveByIDResponse struct {
	InvestigationID string `json:"investigationId"`
	ChatID          string `json:"chatId"`
}

// Message is a shared reply shape for pause/resume/regenerate-report.
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
