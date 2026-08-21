package cloudmonitoring_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudmonitoring"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	gcmclient "github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	base := dsquery.ExploreQuery{
		DatasourceUID:  "gcm-uid",
		DatasourceType: "stackdriver",
		OrgID:          1,
	}
	req := gcmclient.QueryRequest{
		Project:    "my-project",
		MetricType: "compute.googleapis.com/instance/cpu/utilization",
		Reducer:    "REDUCE_NONE",
		Aligner:    "ALIGN_MEAN",
	}

	t.Run("builds explore link", func(t *testing.T) {
		got := cloudmonitoring.QueryExploreURL("https://mystack.grafana.net", base, req)
		require.NotEmpty(t, got)

		u := mustParseURL(t, got)
		assert.Equal(t, "/explore", u.Path)
		params := u.Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))

		pane := decodePane(t, params.Get("panes"))
		assert.Equal(t, "gcm-uid", pane["datasource"])
		assert.Equal(t, map[string]any{"from": "now-1h", "to": "now"}, pane["range"])

		query := paneQuery(t, pane)
		assert.Equal(t, "A", query["refId"])
		assert.Equal(t, "timeSeriesList", query["queryType"])
		assert.Equal(t, map[string]any{"type": "stackdriver", "uid": "gcm-uid"}, query["datasource"])

		tsl, ok := query["timeSeriesList"].(map[string]any)
		require.True(t, ok, "timeSeriesList must be an object")
		assert.Equal(t, "my-project", tsl["projectName"])
		assert.Equal(t, "REDUCE_NONE", tsl["crossSeriesReducer"])
		assert.Equal(t, "ALIGN_MEAN", tsl["perSeriesAligner"])
		assert.Equal(t, "cloud-monitoring-auto", tsl["alignmentPeriod"])
		assert.Equal(t, []any{}, tsl["groupBys"])
		assert.Equal(t, []any{
			"metric.type", "=", "compute.googleapis.com/instance/cpu/utilization",
		}, tsl["filters"])
	})

	t.Run("mirrors the client query model", func(t *testing.T) {
		got := cloudmonitoring.QueryExploreURL("https://mystack.grafana.net", base, req)
		require.NotEmpty(t, got)

		pane := decodePane(t, mustParseURL(t, got).Query().Get("panes"))

		// The pane query must match the request body the client sends. Compare
		// against the shared builder round-tripped through JSON so numeric
		// types line up.
		wantJSON, err := json.Marshal(gcmclient.BuildQueryModel("gcm-uid", req))
		require.NoError(t, err)
		var want map[string]any
		require.NoError(t, json.Unmarshal(wantJSON, &want))

		assert.Equal(t, want, paneQuery(t, pane))
	})

	t.Run("encodes filters and group-bys", func(t *testing.T) {
		got := cloudmonitoring.QueryExploreURL("https://mystack.grafana.net", base, gcmclient.QueryRequest{
			Project:         "my-project",
			MetricType:      "compute.googleapis.com/instance/cpu/utilization",
			Reducer:         "REDUCE_MEAN",
			Aligner:         "ALIGN_RATE",
			AlignmentPeriod: "+60s",
			GroupBys:        []string{"resource.label.instance_name"},
			Filters: map[string]string{
				"resource.label.zone":        "us-east1-b",
				"metric.label.instance_name": "vm-1",
			},
		})
		require.NotEmpty(t, got)

		pane := decodePane(t, mustParseURL(t, got).Query().Get("panes"))
		tsl, ok := paneQuery(t, pane)["timeSeriesList"].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "+60s", tsl["alignmentPeriod"])
		assert.Equal(t, "REDUCE_MEAN", tsl["crossSeriesReducer"])
		assert.Equal(t, "ALIGN_RATE", tsl["perSeriesAligner"])
		assert.Equal(t, []any{"resource.label.instance_name"}, tsl["groupBys"])

		// Filters are a flat, ANDed triplet list with sorted label keys.
		assert.Equal(t, []any{
			"metric.type", "=", "compute.googleapis.com/instance/cpu/utilization",
			"AND", "metric.label.instance_name", "=", "vm-1",
			"AND", "resource.label.zone", "=", "us-east1-b",
		}, tsl["filters"])
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		withRange := base
		withRange.From = "2026-05-10T10:00:00Z"
		withRange.To = "2026-05-10T11:00:00Z"

		got := cloudmonitoring.QueryExploreURL("https://mystack.grafana.net", withRange, req)
		require.NotEmpty(t, got)

		pane := decodePane(t, mustParseURL(t, got).Query().Get("panes"))
		assert.Equal(t, map[string]any{
			"from": "2026-05-10T10:00:00Z",
			"to":   "2026-05-10T11:00:00Z",
		}, pane["range"])
	})

	t.Run("omits orgId when unset", func(t *testing.T) {
		noOrg := base
		noOrg.OrgID = 0

		got := cloudmonitoring.QueryExploreURL("https://mystack.grafana.net", noOrg, req)
		require.NotEmpty(t, got)
		assert.Empty(t, mustParseURL(t, got).Query().Get("orgId"))
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		tests := []struct {
			name string
			host string
			base dsquery.ExploreQuery
			req  gcmclient.QueryRequest
		}{
			{name: "empty host", host: "", base: base, req: req},
			{name: "blank host", host: "   ", base: base, req: req},
			{name: "no scheme", host: "mystack.grafana.net", base: base, req: req},
			{
				name: "missing datasource uid",
				host: "https://mystack.grafana.net",
				base: dsquery.ExploreQuery{DatasourceType: "stackdriver"},
				req:  req,
			},
			{
				name: "missing project",
				host: "https://mystack.grafana.net",
				base: base,
				req:  gcmclient.QueryRequest{MetricType: "compute.googleapis.com/instance/cpu/utilization"},
			},
			{
				name: "blank project",
				host: "https://mystack.grafana.net",
				base: base,
				req:  gcmclient.QueryRequest{Project: "  ", MetricType: "compute.googleapis.com/instance/cpu/utilization"},
			},
			{
				name: "missing metric type",
				host: "https://mystack.grafana.net",
				base: base,
				req:  gcmclient.QueryRequest{Project: "my-project"},
			},
			{
				name: "blank metric type",
				host: "https://mystack.grafana.net",
				base: base,
				req:  gcmclient.QueryRequest{Project: "my-project", MetricType: " "},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Empty(t, cloudmonitoring.QueryExploreURL(tt.host, tt.base, tt.req))
			})
		}
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// decodePane unmarshals the panes parameter and returns the single gcx pane.
func decodePane(t *testing.T, panesJSON string) map[string]any {
	t.Helper()
	require.NotEmpty(t, panesJSON)

	var panes map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(panesJSON), &panes))
	require.Len(t, panes, 1)

	pane, ok := panes[dsquery.DefaultExplorePaneID]
	require.True(t, ok, "pane %q must exist", dsquery.DefaultExplorePaneID)
	return pane
}

// paneQuery returns the single query object inside a pane.
func paneQuery(t *testing.T, pane map[string]any) map[string]any {
	t.Helper()

	queries, ok := pane["queries"].([]any)
	require.True(t, ok, "pane queries must be a list")
	require.Len(t, queries, 1)

	query, ok := queries[0].(map[string]any)
	require.True(t, ok, "pane query must be an object")
	return query
}
