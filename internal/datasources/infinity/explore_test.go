package infinity_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/infinity"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	base := dsquery.ExploreQuery{
		DatasourceUID:  "inf-uid",
		DatasourceType: "yesoreyeram-infinity-datasource",
		Expr:           "$.items",
		OrgID:          1,
	}

	t.Run("builds explore link with root selector", func(t *testing.T) {
		got := infinity.QueryExploreURL("https://mystack.grafana.net", base)
		require.NotEmpty(t, got)
		u, err := url.Parse(got)
		require.NoError(t, err)
		assert.Equal(t, "/explore", u.Path)
		params := u.Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))
		panes := params.Get("panes")
		assert.Contains(t, panes, `"datasource":"inf-uid"`)
		assert.Contains(t, panes, `"datasource":{"type":"yesoreyeram-infinity-datasource","uid":"inf-uid"}`)
		assert.Contains(t, panes, `"root_selector":"$.items"`)
		assert.Contains(t, panes, `"type":"json"`)
		assert.Contains(t, panes, `"source":"url"`)
		assert.Contains(t, panes, `"parser":"backend"`)
		assert.Contains(t, panes, `"format":"table"`)
		assert.Contains(t, panes, `"from":"now-1h"`)
		assert.Contains(t, panes, `"to":"now"`)
	})

	t.Run("empty expression still builds a link", func(t *testing.T) {
		q := base
		q.Expr = ""
		got := infinity.QueryExploreURL("https://mystack.grafana.net", q)
		require.NotEmpty(t, got)
		u, err := url.Parse(got)
		require.NoError(t, err)
		assert.Contains(t, u.Query().Get("panes"), `"root_selector":""`)
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		q := base
		q.From = "2026-05-10T10:00:00Z"
		q.To = "2026-05-10T11:00:00Z"
		u, err := url.Parse(infinity.QueryExploreURL("https://mystack.grafana.net", q))
		require.NoError(t, err)
		panes := u.Query().Get("panes")
		assert.Contains(t, panes, `"from":"2026-05-10T10:00:00Z"`)
		assert.Contains(t, panes, `"to":"2026-05-10T11:00:00Z"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, infinity.QueryExploreURL("", base))
		noUID := base
		noUID.DatasourceUID = ""
		assert.Empty(t, infinity.QueryExploreURL("https://mystack.grafana.net", noUID))
	})
}
