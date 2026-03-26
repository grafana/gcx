package fleet

import "time"

// GetResourceName returns the pipeline ID.
func (p Pipeline) GetResourceName() string { return p.ID }

// SetResourceName restores the pipeline ID.
func (p *Pipeline) SetResourceName(name string) { p.ID = name }

// Pipeline represents a Fleet Management pipeline.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Pipeline struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Contents string         `json:"contents"`
	Matchers []string       `json:"matchers,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GetResourceName returns the collector ID.
func (c Collector) GetResourceName() string { return c.ID }

// SetResourceName restores the collector ID.
func (c *Collector) SetResourceName(name string) { c.ID = name }

// Collector represents a Fleet Management collector.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Collector struct {
	ID               string            `json:"id,omitempty"`
	Name             string            `json:"name,omitempty"`
	RemoteAttributes map[string]string `json:"remote_attributes,omitempty"`
	LocalAttributes  map[string]string `json:"local_attributes,omitempty"`
	CollectorType    string            `json:"collector_type,omitempty"`
	Enabled          *bool             `json:"enabled,omitempty"`
	CreatedAt        *time.Time        `json:"created_at,omitempty"`
	UpdatedAt        *time.Time        `json:"updated_at,omitempty"`
	MarkedInactiveAt *time.Time        `json:"marked_inactive_at,omitempty"`
}

// Limits represents tenant limits for a Fleet Management stack.
type Limits struct {
	Collectors                 *int64 `json:"collectors,omitempty"`
	Pipelines                  *int64 `json:"pipelines,omitempty"`
	RequestsPerSecondCollector *int64 `json:"requests_per_second_collector,omitempty"`
	RequestsPerSecondAPI       *int64 `json:"requests_per_second_api,omitempty"`
}
