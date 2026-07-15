package alert_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestRulerRuleGroup_Validate(t *testing.T) {
	validAlert := alert.RulerRule{Alert: "HighErrors", Expr: `rate(errors_total[5m]) > 0.1`, For: "5m"}
	validRecord := alert.RulerRule{Record: "job:errors:rate5m", Expr: `rate(errors_total[5m])`}

	tests := []struct {
		name    string
		group   alert.RulerRuleGroup
		promQL  bool
		wantErr string
	}{
		{
			name:  "valid alerting and recording rules",
			group: alert.RulerRuleGroup{Name: "g", Interval: "1m", Rules: []alert.RulerRule{validAlert, validRecord}},
		},
		{
			name:   "valid with PromQL validation",
			group:  alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{validAlert}},
			promQL: true,
		},
		{
			name:    "missing group name",
			group:   alert.RulerRuleGroup{Rules: []alert.RulerRule{validAlert}},
			wantErr: "no name",
		},
		{
			name:    "no rules",
			group:   alert.RulerRuleGroup{Name: "g"},
			wantErr: "no rules",
		},
		{
			name:    "invalid interval",
			group:   alert.RulerRuleGroup{Name: "g", Interval: "soon", Rules: []alert.RulerRule{validAlert}},
			wantErr: "invalid interval",
		},
		{
			name:    "neither record nor alert",
			group:   alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{{Expr: "up"}}},
			wantErr: "either `record` or `alert`",
		},
		{
			name: "both record and alert",
			group: alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{
				{Record: "r", Alert: "a", Expr: "up"},
			}},
			wantErr: "only one of",
		},
		{
			name:    "empty expr",
			group:   alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{{Alert: "a"}}},
			wantErr: "empty `expr`",
		},
		{
			name: "recording rule with for",
			group: alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{
				{Record: "r", Expr: "up", For: "5m"},
			}},
			wantErr: "must not set `for`",
		},
		{
			name: "recording rule with annotations",
			group: alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{
				{Record: "r", Expr: "up", Annotations: map[string]string{"a": "b"}},
			}},
			wantErr: "must not set `annotations`",
		},
		{
			name: "invalid for duration",
			group: alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{
				{Alert: "a", Expr: "up", For: "later"},
			}},
			wantErr: "invalid `for` duration",
		},
		{
			name: "invalid PromQL rejected when promQL is on",
			group: alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{
				{Alert: "a", Expr: "rate(up[5m"},
			}},
			promQL:  true,
			wantErr: "invalid PromQL",
		},
		{
			name: "LogQL accepted when promQL is off",
			group: alert.RulerRuleGroup{Name: "g", Rules: []alert.RulerRule{
				{Alert: "a", Expr: `sum(rate({app="foo"} |= "error" [5m])) > 10`},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.group.Validate(tt.promQL)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRulerApplyInput_RuleGroups(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr string
	}{
		{
			name: "standard Prometheus rules file",
			input: `groups:
  - name: one
    rules:
      - alert: A
        expr: up == 0
  - name: two
    rules:
      - record: r:up
        expr: up`,
			want: 2,
		},
		{
			name: "bare single group",
			input: `name: standalone
interval: 1m
rules:
  - alert: A
    expr: up == 0`,
			want: 1,
		},
		{
			name:    "empty object",
			input:   `{}`,
			wantErr: "neither",
		},
		{
			name: "mixed shapes rejected",
			input: `name: standalone
groups:
  - name: one
    rules:
      - alert: A
        expr: up == 0`,
			wantErr: "mixes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input alert.RulerApplyInput
			require.NoError(t, yaml.Unmarshal([]byte(tt.input), &input))
			groups, err := input.RuleGroups()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, groups, tt.want)
		})
	}
}

func TestRulerSubtypeForDatasourceType(t *testing.T) {
	tests := []struct {
		dsType  string
		want    string
		wantErr bool
	}{
		{dsType: "prometheus", want: "mimir"},
		{dsType: "grafana-amazonprometheus-datasource", want: "mimir"},
		{dsType: "loki", want: ""},
		{dsType: "mysql", wantErr: true},
		{dsType: "tempo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.dsType, func(t *testing.T) {
			got, err := alert.RulerSubtypeForDatasourceType(tt.dsType)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no ruler API")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
