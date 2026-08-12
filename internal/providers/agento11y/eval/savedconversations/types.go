package savedconversations

import "time"

// SavedConversation is the bookmark record for a live conversation.
type SavedConversation struct {
	TenantID       string            `json:"tenant_id,omitempty"`
	SavedID        string            `json:"saved_id"`
	ConversationID string            `json:"conversation_id"`
	Name           string            `json:"name"`
	Source         string            `json:"source,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	SavedBy        string            `json:"saved_by,omitempty"`
	CreatedAt      time.Time         `json:"created_at,omitzero"`
	UpdatedAt      time.Time         `json:"updated_at,omitzero"`

	// Enrichment fields populated by the server when listing.
	GenerationCount int               `json:"generation_count,omitempty"`
	TotalTokens     int64             `json:"total_tokens,omitempty"`
	AgentNames      []string          `json:"agent_names,omitempty"`
	Models          []string          `json:"models,omitempty"`
	ModelProviders  map[string]string `json:"model_providers,omitempty"`
}

type SaveRequest struct {
	SavedID        string            `json:"saved_id"`
	ConversationID string            `json:"conversation_id"`
	Name           string            `json:"name"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// CollectionRef is a slim view of a collection returned by the reverse-lookup
// endpoint, with just the columns the table codec renders.
type CollectionRef struct {
	CollectionID string    `json:"collection_id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	MemberCount  int       `json:"member_count,omitempty"`
	CreatedBy    string    `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitzero"`
	UpdatedAt    time.Time `json:"updated_at,omitzero"`
}
