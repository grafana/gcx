package metrics

import "github.com/grafana/gcx/internal/resources/adapter"

// AutoApplyConfig holds the auto-apply configuration for a segment.
type AutoApplyConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// MetricSegment represents an Adaptive Metrics segment (a scoped rule grouping).
//
//nolint:recvcheck // GetResourceName uses value receiver per ResourceNamer; SetResourceName needs pointer.
type MetricSegment struct {
	ID                            string           `json:"id,omitempty" yaml:"id,omitempty"`
	Name                          string           `json:"name" yaml:"name"`
	Selector                      string           `json:"selector" yaml:"selector"`
	FallbackToDefault             bool             `json:"fallback_to_default" yaml:"fallback_to_default"`
	AutoApply                     *AutoApplyConfig `json:"auto_apply,omitempty" yaml:"auto_apply,omitempty"`
	RecommendationConfigurationID *string          `json:"recommendation_configuration_id,omitempty" yaml:"recommendation_configuration_id,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer.
func (s MetricSegment) GetResourceName() string { return s.ID }

// SetResourceName implements adapter.ResourceIdentity.
func (s *MetricSegment) SetResourceName(name string) { s.ID = name }

// Compile-time assertion.
var _ adapter.ResourceIdentity = &MetricSegment{}

// MetricExemption represents an Adaptive Metrics recommendation exemption.
//
//nolint:recvcheck // GetResourceName uses value receiver per ResourceNamer; SetResourceName needs pointer.
type MetricExemption struct {
	ID                     string   `json:"id,omitempty" yaml:"id,omitempty"`
	Metric                 string   `json:"metric" yaml:"metric"`
	MatchType              string   `json:"match_type" yaml:"match_type"`
	KeepLabels             []string `json:"keep_labels,omitempty" yaml:"keep_labels,omitempty"`
	DisableRecommendations bool     `json:"disable_recommendations" yaml:"disable_recommendations"`
	Reason                 string   `json:"reason" yaml:"reason"`
	ManagedBy              string   `json:"managed_by,omitempty" yaml:"managed_by,omitempty"`
	ExpiresAt              string   `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	ActiveInterval         string   `json:"active_interval" yaml:"active_interval"`
	CreatedAt              string   `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt              string   `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer.
func (e MetricExemption) GetResourceName() string { return e.ID }

// SetResourceName implements adapter.ResourceIdentity.
func (e *MetricExemption) SetResourceName(name string) { e.ID = name }

// Compile-time assertion.
var _ adapter.ResourceIdentity = &MetricExemption{}

// ExemptionsBySegmentEntry groups exemptions by their segment (for cross-segment listing).
type ExemptionsBySegmentEntry struct {
	Segment    MetricSegment     `json:"segment"`
	Exemptions []MetricExemption `json:"exemptions"`
}

// MetricRule represents a metric aggregation rule.
//
//nolint:recvcheck // GetResourceName uses value receiver per ResourceNamer; SetResourceName needs pointer.
type MetricRule struct {
	Metric              string   `json:"metric" yaml:"metric"`
	MatchType           string   `json:"match_type,omitempty" yaml:"match_type,omitempty"`
	Drop                bool     `json:"drop,omitempty" yaml:"drop,omitempty"`
	KeepLabels          []string `json:"keep_labels,omitempty" yaml:"keep_labels,omitempty"`
	DropLabels          []string `json:"drop_labels,omitempty" yaml:"drop_labels,omitempty"`
	Aggregations        []string `json:"aggregations,omitempty" yaml:"aggregations,omitempty"`
	AggregationInterval string   `json:"aggregation_interval,omitempty" yaml:"aggregation_interval,omitempty"`
	AggregationDelay    string   `json:"aggregation_delay,omitempty" yaml:"aggregation_delay,omitempty"`
	ManagedBy           string   `json:"managed_by,omitempty" yaml:"managed_by,omitempty"`
}

// MetricRecommendation represents a recommendation for a metric aggregation rule.
type MetricRecommendation struct {
	MetricRule `yaml:",inline"`

	RecommendedAction      string   `json:"recommended_action" yaml:"recommended_action"`
	UsagesInRules          int      `json:"usages_in_rules" yaml:"usages_in_rules"`
	UsagesInQueries        int      `json:"usages_in_queries" yaml:"usages_in_queries"`
	UsagesInDashboards     int      `json:"usages_in_dashboards" yaml:"usages_in_dashboards"`
	KeptLabels             []string `json:"kept_labels,omitempty" yaml:"kept_labels,omitempty"`
	RawSeriesCount         int      `json:"raw_series_count,omitempty" yaml:"raw_series_count,omitempty"`
	CurrentSeriesCount     int      `json:"current_series_count,omitempty" yaml:"current_series_count,omitempty"`
	RecommendedSeriesCount int      `json:"recommended_series_count,omitempty" yaml:"recommended_series_count,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer.
func (r MetricRule) GetResourceName() string { return r.Metric }

// SetResourceName implements adapter.ResourceIdentity.
func (r *MetricRule) SetResourceName(name string) { r.Metric = name }

// Compile-time assertion.
var _ adapter.ResourceIdentity = &MetricRule{}

// ToRule extracts the MetricRule from a recommendation.
func (r MetricRecommendation) ToRule() MetricRule {
	return r.MetricRule
}
