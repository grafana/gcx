package logs

import (
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// Segment represents per-dimension volume and drop-rate data within a recommendation.
type Segment struct {
	Volume              uint64   `json:"volume"`
	IngestedLines       uint64   `json:"ingested_lines"`
	ConfiguredDropRate  *float32 `json:"configured_drop_rate,omitempty"`
	QueriedLines        uint64   `json:"queried_lines"`
	RecommendedDropRate float64  `json:"recommended_drop_rate"`
}

// LogRecommendation represents an adaptive log pattern recommendation.
type LogRecommendation struct {
	Pattern             string             `json:"pattern"`
	Tokens              []string           `json:"tokens,omitempty"`
	Locked              bool               `json:"locked"`
	ConfiguredDropRate  float32            `json:"configured_drop_rate"`
	Volume              uint64             `json:"volume,omitempty"`
	IngestedLines       uint64             `json:"ingested_lines,omitempty"`
	QueriedLines        uint64             `json:"queried_lines,omitempty"`
	RecommendedDropRate float64            `json:"recommended_drop_rate,omitempty"`
	Superseded          bool               `json:"superseded,omitempty"`
	IsEarly             bool               `json:"is_early,omitempty"`
	Levels              []string           `json:"levels,omitempty"`
	Segments            map[string]Segment `json:"segments,omitempty"`
}

// Label returns the best human-readable identifier.
func (r *LogRecommendation) Label() string {
	if len(r.Tokens) > 0 {
		return strings.Join(r.Tokens, "")
	}
	if len(r.Segments) > 0 {
		keys := make([]string, 0, len(r.Segments))
		for k := range r.Segments {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "{" + strings.Join(keys, ", ") + "}"
	}
	return "(unknown)"
}

// Exemption represents a log stream exemption.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Exemption struct {
	ID             string `json:"id,omitempty"`
	StreamSelector string `json:"stream_selector"`
	Reason         string `json:"reason,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	ManagedBy      string `json:"managed_by,omitempty"`
	ActiveInterval string `json:"active_interval,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer for TypedCRUD compatibility.
func (e Exemption) GetResourceName() string { return e.ID }

// SetResourceName implements adapter.ResourceIdentity for TypedCRUD compatibility.
func (e *Exemption) SetResourceName(name string) { e.ID = name }

// Compile-time assertion: Exemption implements adapter.ResourceIdentity.
var _ adapter.ResourceIdentity = &Exemption{}

// LogSegment represents an adaptive logs segment configuration.
// Named LogSegment to avoid collision with the Segment type (per-dimension stats within a recommendation).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type LogSegment struct {
	ID                string `json:"id,omitempty"`
	Selector          string `json:"selector,omitempty"`
	Name              string `json:"name"`
	FallbackToDefault bool   `json:"fallback_to_default,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
	IsEarly           bool   `json:"is_early,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer for TypedCRUD compatibility.
func (s LogSegment) GetResourceName() string { return s.ID }

// SetResourceName implements adapter.ResourceIdentity for TypedCRUD compatibility.
func (s *LogSegment) SetResourceName(name string) { s.ID = name }

// Compile-time assertion: LogSegment implements adapter.ResourceIdentity.
var _ adapter.ResourceIdentity = &LogSegment{}
