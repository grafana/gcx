//nolint:testpackage // White-box test: exercises unexported conversion helpers.
package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleV2_Conversion(t *testing.T) {
	tests := []struct {
		name string
		v1   MetricRule
		v2   ruleV2
	}{
		{
			name: "aggregate dropping labels",
			v1:   MetricRule{Metric: "m", MatchType: "exact", Aggregations: []string{"sum", "count"}, DropLabels: []string{"pod"}},
			v2:   ruleV2{Metric: "m", MatchType: "exact", Action: "aggregate", Functions: []string{"sum", "count"}, LabelPolicy: &labelPolicyV2{Mode: "drop", Labels: []string{"pod"}}},
		},
		{
			name: "aggregate keeping labels with interval and delay",
			v1:   MetricRule{Metric: "m", MatchType: "prefix", Aggregations: []string{"max"}, KeepLabels: []string{"job", "instance"}, AggregationInterval: "1m", AggregationDelay: "5m"},
			v2:   ruleV2{Metric: "m", MatchType: "prefix", Action: "aggregate", Functions: []string{"max"}, Interval: "1m", Delay: "5m", LabelPolicy: &labelPolicyV2{Mode: "keep", Labels: []string{"job", "instance"}}},
		},
		{
			name: "drop",
			v1:   MetricRule{Metric: "m", MatchType: "suffix", Drop: true},
			v2:   ruleV2{Metric: "m", MatchType: "suffix", Action: "drop"},
		},
		{
			name: "empty match type is preserved",
			v1:   MetricRule{Metric: "m", Aggregations: []string{"sum"}, DropLabels: []string{"a"}},
			v2:   ruleV2{Metric: "m", Action: "aggregate", Functions: []string{"sum"}, LabelPolicy: &labelPolicyV2{Mode: "drop", Labels: []string{"a"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toRuleV2(tt.v1)
			require.NoError(t, err)
			assert.Equal(t, tt.v2, got)
			assert.Equal(t, tt.v1, fromRuleV2(got))
		})
	}
}

func TestToRuleV2_Errors(t *testing.T) {
	tests := []struct {
		name string
		rule MetricRule
		want string
	}{
		{"both label kinds", MetricRule{Metric: "m", Aggregations: []string{"sum"}, KeepLabels: []string{"a"}, DropLabels: []string{"b"}}, "both dropped and kept labels"},
		{"no labels", MetricRule{Metric: "m", Aggregations: []string{"sum"}}, "no labels to aggregate on"},
		{"no aggregations", MetricRule{Metric: "m", KeepLabels: []string{"a"}}, "at least one aggregation"},
		{"drop with interval", MetricRule{Metric: "m", Drop: true, AggregationInterval: "1m"}, "interval or delay"},
		{"drop with delay", MetricRule{Metric: "m", Drop: true, AggregationDelay: "5m"}, "interval or delay"},
		{"drop with aggregations", MetricRule{Metric: "m", Drop: true, Aggregations: []string{"sum"}}, "drop and aggregate"},
		{"drop with keep labels", MetricRule{Metric: "m", Drop: true, KeepLabels: []string{"a"}}, "drop and aggregate"},
		{"drop with drop labels", MetricRule{Metric: "m", Drop: true, DropLabels: []string{"a"}}, "drop and aggregate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := toRuleV2(tt.rule)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRuleV2_ManagedByNotMapped(t *testing.T) {
	got, err := toRuleV2(MetricRule{Metric: "m", MatchType: "exact", Drop: true, ManagedBy: "terraform"})
	require.NoError(t, err)
	assert.Empty(t, fromRuleV2(got).ManagedBy)
}

func TestFromRuleV2_UnknownLabelMode(t *testing.T) {
	got := fromRuleV2(ruleV2{Metric: "m", Action: "aggregate", Functions: []string{"sum"}, LabelPolicy: &labelPolicyV2{Mode: "other", Labels: []string{"a"}}})
	assert.Empty(t, got.KeepLabels)
	assert.Empty(t, got.DropLabels)
}

func TestValidateRuleRef(t *testing.T) {
	tests := []struct {
		name       string
		segment    string
		metric     string
		needMetric bool
		wantErr    string
	}{
		{name: "default segment and metric", metric: "http_requests_total", needMetric: true},
		{name: "custom segment", segment: "team-a"},
		{name: "metric with colons", metric: "job:http_requests:rate5m", needMetric: true},
		{name: "metric not required", metric: ""},
		{name: "blank segment", segment: "  ", wantErr: "invalid segment"},
		{name: "dot segment", segment: ".", wantErr: "invalid segment"},
		{name: "dot-dot segment", segment: "..", wantErr: "invalid segment"},
		{name: "empty metric", needMetric: true, wantErr: "invalid metric"},
		{name: "blank metric", metric: " ", needMetric: true, wantErr: "invalid metric"},
		{name: "dot metric", metric: ".", needMetric: true, wantErr: "invalid metric"},
		{name: "dot-dot metric", metric: "..", needMetric: true, wantErr: "invalid metric"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuleRef(tt.segment, tt.metric, tt.needMetric)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
