package newrelic_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/newrelic"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	query "github.com/grafana/gcx/internal/query/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	req := query.QueryRequest{QueryText: "SELECT count(*) FROM Transaction", AccountID: 123, TimeoutSeconds: 30}
	explore := dsquery.ExploreQuery{
		DatasourceUID: "nr-uid", From: "now-2h", To: "now", OrgID: 1,
	}
	link := newrelic.QueryExploreURL("https://grafana.example.com", explore, req)
	u, err := url.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "/explore", u.Path)
	assert.Equal(t, "1", u.Query().Get("orgId"))
	var panes map[string]struct {
		Datasource string           `json:"datasource"`
		Queries    []map[string]any `json:"queries"`
		Range      map[string]any   `json:"range"`
	}
	require.NoError(t, json.Unmarshal([]byte(u.Query().Get("panes")), &panes))
	require.Len(t, panes, 1)
	for _, pane := range panes {
		assert.Equal(t, "nr-uid", pane.Datasource)
		assert.Equal(t, "now-2h", pane.Range["from"])
		assert.Equal(t, "now", pane.Range["to"])
		require.Len(t, pane.Queries, 1)
		assert.Equal(t, req.QueryText, pane.Queries[0]["queryText"])
		assert.InDelta(t, 123, pane.Queries[0]["accountID"], 0)
		assert.InDelta(t, 30, pane.Queries[0]["timeoutSeconds"], 0)
		assert.Equal(t, map[string]any{"type": "nrgrafanaplugin-newrelic-datasource", "uid": "nr-uid"}, pane.Queries[0]["datasource"])
	}
	assert.Empty(t, newrelic.QueryExploreURL("", explore, req))
}
