package fleet

import "time"

// Pipeline represents a Fleet Management pipeline.
type Pipeline struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Contents string         `json:"contents"`
	Matchers []string       `json:"matchers,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Collector represents a Fleet Management collector.
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
