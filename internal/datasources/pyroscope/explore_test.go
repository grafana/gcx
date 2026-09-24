package pyroscope_test

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	dspyroscope "github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const profileType = "process_cpu:cpu:nanoseconds:cpu:nanoseconds"

type explorePane struct {
	Datasource string            `json:"datasource"`
	Queries    []map[string]any  `json:"queries"`
	Range      map[string]string `json:"range"`
}

func parseExploreURL(t *testing.T, raw string) (*url.URL, explorePane) {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	var panes map[string]explorePane
	require.NoError(t, json.Unmarshal([]byte(u.Query().Get("panes")), &panes))
	require.Len(t, panes, 1)
	pane := panes["gcx"]
	require.Len(t, pane.Queries, 1)
	return u, pane
}

func TestExploreURLs(t *testing.T) {
	start := time.Date(2026, 9, 1, 10, 0, 0, 123000000, time.UTC)
	end := start.Add(time.Hour)
	selector := `{service_name=~"api|worker",namespace="a&b",path="a\\b\"c"}`
	for _, tt := range []struct {
		name  string
		build func(string, string, int64) string
		want  map[string]any
	}{
		{"profile", func(host, uid string, orgID int64) string {
			return dspyroscope.QueryExploreURL(host, uid, orgID, pyroscope.QueryRequest{
				ProfileTypeID: profileType, LabelSelector: selector, Start: start, End: end,
				MaxNodes: 123, SpanIDs: []string{"00f067aa0ba902b7"},
			})
		}, map[string]any{"queryType": "profile", "groupBy": []any{}, "maxNodes": float64(123), "spanSelector": []any{"00f067aa0ba902b7"}}},
		{"metrics", func(host, uid string, orgID int64) string {
			return dspyroscope.MetricsExploreURL(host, uid, orgID, pyroscope.SelectSeriesRequest{
				ProfileTypeID: profileType, LabelSelector: selector, Start: start, End: end,
				GroupBy: []string{"service_name", "namespace"}, Limit: 20,
			})
		}, map[string]any{"queryType": "metrics", "groupBy": []any{"service_name", "namespace"}, "limit": float64(20)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u, pane := parseExploreURL(t, tt.build("https://example.com/grafana/", "pyro-uid", 7))
			assert.Equal(t, "/grafana/explore", u.Path)
			assert.Equal(t, "1", u.Query().Get("schemaVersion"))
			assert.Equal(t, "7", u.Query().Get("orgId"))
			assert.Equal(t, "pyro-uid", pane.Datasource)
			assert.Equal(t, "1788256800123", pane.Range["from"])
			assert.Equal(t, "1788260400123", pane.Range["to"])
			q := pane.Queries[0]
			assert.Equal(t, "A", q["refId"])
			assert.Equal(t, profileType, q["profileTypeId"])
			assert.Equal(t, selector, q["labelSelector"])
			assert.Equal(t, map[string]any{"type": "grafana-pyroscope-datasource", "uid": "pyro-uid"}, q["datasource"])
			for key, value := range tt.want {
				assert.Equal(t, value, q[key], key)
			}
			for _, host := range []string{"", "file:///tmp/grafana", "not a URL"} {
				assert.Empty(t, tt.build(host, "pyro-uid", 0))
			}
			assert.Empty(t, tt.build("https://example.com", "", 0))
			u, _ = parseExploreURL(t, tt.build("http://localhost:3000", "pyro-uid", 0))
			assert.Empty(t, u.Query().Get("orgId"))
		})
	}
}
