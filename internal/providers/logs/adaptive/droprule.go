package logs

import (
	"encoding/json"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// GlobalDropRuleSegmentID is the segment ID used for tenant-wide adaptive log drop rules.
// The drop-rules CLI commands always use this segment for now.
const GlobalDropRuleSegmentID = "__global__"

// DropRuleListQuery filters the adaptive logs drop rules list endpoint.
type DropRuleListQuery struct {
	SegmentID string
	// ExpirationFilter is one of: all, active, expired. Empty defaults to "all" in the client.
	ExpirationFilter string
}

// DropRule is an Adaptive Logs drop rule (HTTP: /adaptive-logs/drop-rules).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type DropRule struct {
	ID        string          `json:"id,omitempty"`
	TenantID  string          `json:"tenant_id,omitempty"`
	SegmentID string          `json:"segment_id,omitempty"`
	Version   int             `json:"version"`
	Name      string          `json:"name"`
	Body      json.RawMessage `json:"body,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
	ExpiresAt string          `json:"expires_at,omitempty"`
	Disabled  bool            `json:"disabled,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer for TypedCRUD compatibility.
func (d DropRule) GetResourceName() string { return d.ID }

// SetResourceName implements adapter.ResourceIdentity for TypedCRUD compatibility.
func (d *DropRule) SetResourceName(name string) { d.ID = name }

// Compile-time assertion: DropRule implements adapter.ResourceIdentity.
var _ adapter.ResourceIdentity = &DropRule{}

func dropRuleCreatePayload(dr *DropRule) ([]byte, error) {
	type payload struct {
		SegmentID string          `json:"segment_id"`
		Version   int             `json:"version"`
		Name      string          `json:"name"`
		Body      json.RawMessage `json:"body"`
		ExpiresAt string          `json:"expires_at,omitempty"`
		Disabled  bool            `json:"disabled,omitempty"`
	}
	p := payload{
		SegmentID: dr.SegmentID,
		Version:   dr.Version,
		Name:      dr.Name,
		Body:      dr.Body,
		ExpiresAt: dr.ExpiresAt,
		Disabled:  dr.Disabled,
	}
	return json.Marshal(p)
}

func dropRuleUpdatePayload(dr *DropRule) ([]byte, error) {
	type payload struct {
		Version   int             `json:"version"`
		Name      string          `json:"name"`
		Body      json.RawMessage `json:"body"`
		ExpiresAt string          `json:"expires_at,omitempty"`
		Disabled  bool            `json:"disabled,omitempty"`
	}
	p := payload{
		Version:   dr.Version,
		Name:      dr.Name,
		Body:      dr.Body,
		ExpiresAt: dr.ExpiresAt,
		Disabled:  dr.Disabled,
	}
	return json.Marshal(p)
}
