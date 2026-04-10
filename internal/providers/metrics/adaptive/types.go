package metrics

import "github.com/grafana/gcx/internal/resources/adapter"

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
