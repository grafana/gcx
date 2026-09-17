package kg_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTableGolden(t *testing.T) {
	rules := []unstructured.Unstructured{ruleObj("mixed-rules", []map[string]any{{"name": "group", "rules": []any{map[string]any{"alert": "Alert"}, map[string]any{"record": "metric"}}}}), ruleObj("empty", nil)}
	for _, tc := range []struct {
		name  string
		codec format.Codec
		rows  any
		empty any
	}{
		{"rules", kg.RuleTable().Codec("table"), rules, []unstructured.Unstructured{}},
		{"rules_wide", kg.RuleTable().Codec("wide"), rules, []unstructured.Unstructured{}},
		{"model_rule_names", kg.ModelRulesNameTable().Codec("table"), []string{"service-model", ""}, []string{}},
		{"notifications", kg.NotificationTable().Codec("table"), []kg.AlertConfig{{Name: "notify", MatchLabels: map[string]string{"site": "west", "env": "prod"}, For: "5m", Silenced: true}, {}}, []kg.AlertConfig{}},
		{"suppressions", kg.SuppressionTable().Codec("table"), []kg.Suppression{{Name: "maintenance", MatchLabels: map[string]string{"site": "west", "env": "prod"}}, {}}, []kg.Suppression{}},
		{"entities", kg.EntityTable().Codec("table"), []kg.SearchResult{{Type: "Service", EntityType: "fallback", Name: "checkout", Active: true, Scope: map[string]string{"site": "west", "env": "prod"}}, {EntityType: "Pod", Name: "worker"}, {}}, []kg.SearchResult{}},
		{"alert_correlate", kg.AlertCorrelateTable().Codec("table"), []kg.GraphEntity{{Type: "Service", Name: "checkout", Scope: map[string]string{"site": "west", "env": "prod"}, ConnectedEntityTypes: map[string]int{"Service": 2, "Pod": 10}}, {}}, []kg.GraphEntity{}},
		{"quality_list", kg.QualityReportListTable().Codec("table"), []kg.QualityReportListItem{{EntityName: "checkout", EntityType: "Service", Env: "prod", Namespace: "apps", Site: "west", QualityPercent: 60, FailedCheckIDs: []string{"logs", "metrics"}}, {}}, []kg.QualityReportListItem{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, data := range []struct {
				suffix string
				value  any
			}{{"", tc.rows}, {"_empty", tc.empty}} {
				var buf bytes.Buffer
				require.NoError(t, tc.codec.Encode(&buf, data.value))
				testutils.Golden(t, tc.name+data.suffix, buf.String())
			}
		})
	}
}
