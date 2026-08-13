package collections

import "time"

//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Collection struct {
	// User-provided fields (spec).
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Server-managed fields (stripped from spec on push).
	CollectionID string    `json:"collection_id,omitempty"`
	TenantID     string    `json:"tenant_id,omitempty"`
	CreatedBy    string    `json:"created_by,omitempty"`
	UpdatedBy    string    `json:"updated_by,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitzero"`
	UpdatedAt    time.Time `json:"updated_at,omitzero"`
	MemberCount  int       `json:"member_count,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer.
func (c Collection) GetResourceName() string { return c.CollectionID }

// SetResourceName implements adapter.ResourceIdentity.
func (c *Collection) SetResourceName(name string) { c.CollectionID = name }

// UpdateRequest is the partial-PATCH body for the update endpoint. Pointer
// fields let callers send only the fields they want to change.
type UpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type AddMembersRequest struct {
	SavedIDs []string `json:"saved_ids"`
}
