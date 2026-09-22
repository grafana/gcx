package pyroscope_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/pyroscope"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	got := pyroscope.QueryExploreURL("https://stack.grafana.net", dsquery.ExploreQuery{
		DatasourceUID:  "pyro-uid",
		DatasourceType: "grafana-pyroscope-datasource",
		Expr:           `{service_name="frontend"}`,
		OrgID:          1,
	}, "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
		[]string{"aaa"}, []string{"550e8400-e29b-41d4-a716-446655440000"}, []string{"main"}, 500)
	require.NotEmpty(t, got)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "/explore", u.Path)

	panesRaw := u.Query().Get("panes")
	var panes map[string]struct {
		Datasource string           `json:"datasource"`
		Queries    []map[string]any `json:"queries"`
	}
	require.NoError(t, json.Unmarshal([]byte(panesRaw), &panes))

	pane, ok := panes[dsquery.DefaultExplorePaneID]
	require.True(t, ok)
	require.Len(t, pane.Queries, 1)
	q := pane.Queries[0]

	assert.Equal(t, "profile", q["queryType"])
	assert.Equal(t, `{service_name="frontend"}`, q["labelSelector"])
	assert.Equal(t, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", q["profileTypeId"])
	assert.Equal(t, []any{"aaa"}, q["spanSelector"])
	assert.Equal(t, []any{"550e8400-e29b-41d4-a716-446655440000"}, q["profileIdSelector"])
	assert.Equal(t, []any{"main"}, q["stackTraceSelector"])
	assert.InDelta(t, 500, q["maxNodes"], 0)
	assert.Nil(t, q["traceIdSelector"])
}

func TestQueryExploreURL_FallbackCases(t *testing.T) {
	base := dsquery.ExploreQuery{DatasourceUID: "pyro-uid", Expr: `{service_name="frontend"}`}

	tests := map[string]struct {
		host        string
		query       dsquery.ExploreQuery
		profileType string
	}{
		"missing host":         {"", base, "cpu"},
		"missing datasource":   {"https://stack.grafana.net", dsquery.ExploreQuery{Expr: `{service_name="frontend"}`}, "cpu"},
		"missing expr":         {"https://stack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "pyro-uid"}, "cpu"},
		"missing profile type": {"https://stack.grafana.net", base, ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := pyroscope.QueryExploreURL(tt.host, tt.query, tt.profileType, nil, nil, nil, 0)
			assert.Empty(t, got)
		})
	}
}
