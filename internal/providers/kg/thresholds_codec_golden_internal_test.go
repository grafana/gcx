package kg

import (
	"bytes"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goldenThresholds pairs a fully populated custom threshold with a global one
// that leaves the optional label set empty, so the goldens pin the "-"
// placeholder and the custom-before-global scope ordering as well as the
// columns. Records are deliberately out of order to pin the per-scope sort.
func goldenThresholds() *ThresholdRulesDto {
	return &ThresholdRulesDto{
		CustomThresholds: []Threshold{
			{Record: "asserts:latency:p99:threshold", Expr: "0.5", Active: true, Labels: map[string]string{"b": "2", "a": "1"}},
			{Record: "asserts:latency:average:threshold", Expr: "0.1", Active: false, Labels: map[string]string{"asserts_request_type": "statements"}},
		},
		GlobalThresholds: []Threshold{
			{Record: "asserts:resource:rate:threshold_by_stddev", Expr: "2", Active: true},
		},
	}
}

func TestThresholdsTableGolden(t *testing.T) {
	var narrow bytes.Buffer
	require.NoError(t, thresholdTable().Codec(cmdio.FormatTable).Encode(&narrow, flattenThresholds(goldenThresholds())))
	testutils.Golden(t, "thresholds_table", narrow.String())

	var wide bytes.Buffer
	require.NoError(t, thresholdTable().Codec(cmdio.FormatWide).Encode(&wide, flattenThresholds(goldenThresholds())))
	testutils.Golden(t, "thresholds_wide", wide.String())

	var empty bytes.Buffer
	require.NoError(t, thresholdTable().Codec(cmdio.FormatTable).Encode(&empty, flattenThresholds(&ThresholdRulesDto{})))
	testutils.Golden(t, "thresholds_table_empty", empty.String())
}

// Real global thresholds are indented, multi-line PromQL. Left as-is they put
// newlines inside a cell and break the table apart, so the goldens pin the
// folded, untruncated rendering against the shape a live stack returns.
func TestThresholdsTableGolden_MultilineExpr(t *testing.T) {
	dto := &ThresholdRulesDto{
		GlobalThresholds: []Threshold{{
			Record: "asserts:error:ratio:threshold",
			Active: false,
			Expr: `clamp_max(
  max by (asserts_env, asserts_site, namespace, workload, service, job) (
    quantile_over_time(0.75, asserts:error:ratio[1d] offset 1h)
  ) > on() group_left() (asserts:error:ratio:threshold{asserts_threshold_level=""} > 0),
  scalar(asserts:error:ratio:threshold_automatic_max)
)`,
		}},
	}

	var narrow bytes.Buffer
	require.NoError(t, thresholdTable().Codec(cmdio.FormatTable).Encode(&narrow, flattenThresholds(dto)))
	assert.NotContains(t, strings.TrimSuffix(narrow.String(), "\n"), "\n\n")
	testutils.Golden(t, "thresholds_table_multiline_expr", narrow.String())
}

func TestCompactExpr(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"single line is untouched", "0.1", "0.1"},
		{"multiline expression is folded without clipping", "clamp_max(\n  round(\n    1.1\n  )\n)", "clamp_max( round( 1.1 ) )"},
		{"quoted whitespace is preserved", `sum(metric{label="a  b"})`, `sum(metric{label="a  b"})`},
		{"raw string line break is escaped", "sum(metric{label=`a\nb`})", "sum(metric{label=`a\\nb`})"},
		{"long expression is not clipped", strings.Repeat("a", 100), strings.Repeat("a", 100)},
		{"invalid input still stays on one line", "not valid\n  promql", "not valid promql"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compactExpr(tt.expr))
		})
	}
}

func TestRenderLabels(t *testing.T) {
	assert.Equal(t, "a=simple,b=\"comma,value\",c=\"line\\nbreak\"", renderLabels(map[string]string{
		"c": "line\nbreak",
		"a": "simple",
		"b": "comma,value",
	}))
}
