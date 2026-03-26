package fleet

import "time"

// GetResourceName returns the slug-id composite name for the pipeline.
func (p Pipeline) GetResourceName() string {
	if p.Name != "" && p.ID != "" {
		return slugifyName(p.Name) + "-" + p.ID
	}
	return p.ID
}

// SetResourceName restores the pipeline ID from a slug-id composite name.
func (p *Pipeline) SetResourceName(name string) {
	if id, ok := extractIDFromSlug(name); ok {
		p.ID = id
	} else {
		p.ID = name
	}
}

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

// GetResourceName returns the slug-id composite name for the collector.
func (c Collector) GetResourceName() string {
	if c.Name != "" && c.ID != "" {
		return slugifyName(c.Name) + "-" + c.ID
	}
	return c.ID
}

// SetResourceName restores the collector ID from a slug-id composite name.
func (c *Collector) SetResourceName(name string) {
	if id, ok := extractIDFromSlug(name); ok {
		c.ID = id
	} else {
		c.ID = name
	}
}

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
