package metrics

// MetricRecommendation represents a recommended metric aggregation rule.
type MetricRecommendation struct {
	MetricName   string   `json:"metric"`
	DropLabels   []string `json:"drop_labels,omitempty"`
	Aggregations []string `json:"aggregations,omitempty"`
}

// MetricRule represents a metric aggregation rule.
type MetricRule struct {
	MetricName   string   `json:"metric"`
	DropLabels   []string `json:"drop_labels,omitempty"`
	Aggregations []string `json:"aggregations,omitempty"`
}
