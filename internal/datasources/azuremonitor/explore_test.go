package azuremonitor_test

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/azuremonitor"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	azclient "github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHost   = "https://mystack.grafana.net"
	testDSUID  = "az-uid"
	testDSType = "grafana-azure-monitor-datasource"
)

// decodePane parses an Explore URL and returns the query object and the pane
// range of the single pane it carries.
func decodePane(t *testing.T, raw string) (map[string]any, map[string]any) {
	t.Helper()

	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "/explore", u.Path)
	assert.Equal(t, "1", u.Query().Get("schemaVersion"))

	var panes map[string]struct {
		Datasource string           `json:"datasource"`
		Queries    []map[string]any `json:"queries"`
		Range      map[string]any   `json:"range"`
	}
	require.NoError(t, json.Unmarshal([]byte(u.Query().Get("panes")), &panes))

	pane, ok := panes[dsquery.DefaultExplorePaneID]
	require.True(t, ok, "expected a pane under %q", dsquery.DefaultExplorePaneID)
	assert.Equal(t, testDSUID, pane.Datasource)
	require.Len(t, pane.Queries, 1)

	return pane.Queries[0], pane.Range
}

// assertDatasourceRef checks the datasource reference block inside a query object.
func assertDatasourceRef(t *testing.T, query map[string]any) {
	t.Helper()

	ref, ok := query["datasource"].(map[string]any)
	require.True(t, ok, "query has no datasource block")
	assert.Equal(t, testDSType, ref["type"])
	assert.Equal(t, testDSUID, ref["uid"])
}

func metricsRequest() azclient.QueryRequest {
	start := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	return azclient.QueryRequest{
		Subscription:     "sub-123",
		ResourceGroup:    "my-rg",
		ResourceName:     "mystorage",
		MetricNamespace:  "Microsoft.Storage/storageAccounts",
		MetricName:       "Transactions",
		Aggregation:      "Total",
		TimeGrain:        "auto",
		Region:           "uksouth",
		Top:              "10",
		DimensionFilters: map[string]string{"ApiName": "*"},
		Start:            start,
		End:              start.Add(time.Hour),
	}
}

func TestQueryExploreURL(t *testing.T) {
	t.Run("mirrors the metrics query model", func(t *testing.T) {
		got := azuremonitor.QueryExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
			From:           "2026-05-10T10:00:00Z",
			To:             "2026-05-10T11:00:00Z",
			OrgID:          7,
		}, metricsRequest())
		require.NotEmpty(t, got)

		u, err := url.Parse(got)
		require.NoError(t, err)
		assert.Equal(t, "7", u.Query().Get("orgId"))

		query, paneRange := decodePane(t, got)
		assert.Equal(t, "Azure Monitor", query["queryType"])
		assert.Equal(t, "A", query["refId"])
		// The plugin backend reads the query-level subscription field.
		assert.Equal(t, "sub-123", query["subscription"])
		assertDatasourceRef(t, query)

		azm, ok := query["azureMonitor"].(map[string]any)
		require.True(t, ok, "query has no azureMonitor block")
		assert.Equal(t, "Transactions", azm["metricName"])
		assert.Equal(t, "Microsoft.Storage/storageAccounts", azm["metricNamespace"])
		assert.Equal(t, "Total", azm["aggregation"])
		assert.Equal(t, "auto", azm["timeGrain"])
		assert.Equal(t, "uksouth", azm["region"])
		assert.Equal(t, "10", azm["top"])
		assert.Equal(t, []any{map[string]any{
			"dimension": "ApiName",
			"operator":  "eq",
			"filters":   []any{"*"},
		}}, azm["dimensionFilters"])
		assert.Equal(t, []any{map[string]any{
			"resourceGroup":   "my-rg",
			"resourceName":    "mystorage",
			"metricNamespace": "Microsoft.Storage/storageAccounts",
			"region":          "uksouth",
		}}, azm["resources"])

		// The pane range carries the time span, not the query object.
		assert.Equal(t, "2026-05-10T10:00:00Z", paneRange["from"])
		assert.Equal(t, "2026-05-10T11:00:00Z", paneRange["to"])
		assert.NotContains(t, query, "from")
		assert.NotContains(t, query, "to")
	})

	t.Run("matches the query model the client sends", func(t *testing.T) {
		req := metricsRequest()
		got := azuremonitor.QueryExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
		}, req)
		require.NotEmpty(t, got)

		query, _ := decodePane(t, got)

		want := roundTrip(t, azclient.MetricsQueryModel(testDSUID, req))
		assert.Equal(t, want, query)
	})

	t.Run("defaults the pane range when no time range is set", func(t *testing.T) {
		got := azuremonitor.QueryExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
		}, metricsRequest())
		require.NotEmpty(t, got)

		_, paneRange := decodePane(t, got)
		assert.Equal(t, "now-1h", paneRange["from"])
		assert.Equal(t, "now", paneRange["to"])
	})

	t.Run("carries the resolved default subscription", func(t *testing.T) {
		req := metricsRequest()
		// The command resolves the datasource default subscription before it
		// builds the request, so the link must repeat that value.
		req.Subscription = "default-sub-from-datasource"

		got := azuremonitor.QueryExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
		}, req)
		require.NotEmpty(t, got)

		query, _ := decodePane(t, got)
		assert.Equal(t, "default-sub-from-datasource", query["subscription"])
	})

	t.Run("returns empty for missing required input", func(t *testing.T) {
		link := dsquery.ExploreQuery{DatasourceUID: testDSUID, DatasourceType: testDSType}

		assert.Empty(t, azuremonitor.QueryExploreURL("", link, metricsRequest()))
		assert.Empty(t, azuremonitor.QueryExploreURL("mystack.grafana.net", link, metricsRequest()))
		assert.Empty(t, azuremonitor.QueryExploreURL(testHost, dsquery.ExploreQuery{DatasourceType: testDSType}, metricsRequest()))

		noMetric := metricsRequest()
		noMetric.MetricName = ""
		assert.Empty(t, azuremonitor.QueryExploreURL(testHost, link, noMetric))

		noSub := metricsRequest()
		noSub.Subscription = ""
		assert.Empty(t, azuremonitor.QueryExploreURL(testHost, link, noSub))
	})
}

func logsRequest() azclient.LogsQueryRequest {
	start := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	return azclient.LogsQueryRequest{
		Subscription:  "sub-123",
		ResourceGroup: "my-rg",
		Workspace:     "my-workspace",
		Query:         "AppRequests | take 10",
		Start:         start,
		End:           start.Add(time.Hour),
	}
}

func TestLogsExploreURL(t *testing.T) {
	t.Run("mirrors the log analytics query model", func(t *testing.T) {
		got := azuremonitor.LogsExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
			From:           "now-6h",
			To:             "now",
		}, logsRequest())
		require.NotEmpty(t, got)

		query, paneRange := decodePane(t, got)
		assert.Equal(t, "Azure Log Analytics", query["queryType"])
		assert.Equal(t, "A", query["refId"])
		assertDatasourceRef(t, query)

		ala, ok := query["azureLogAnalytics"].(map[string]any)
		require.True(t, ok, "query has no azureLogAnalytics block")
		assert.Equal(t, "AppRequests | take 10", ala["query"])
		assert.Equal(t, "table", ala["resultFormat"])
		assert.Equal(t, []any{
			"/subscriptions/sub-123/resourceGroups/my-rg/providers/Microsoft.OperationalInsights/workspaces/my-workspace",
		}, ala["resources"])

		assert.Equal(t, "now-6h", paneRange["from"])
		assert.Equal(t, "now", paneRange["to"])
	})

	t.Run("matches the query model the client sends", func(t *testing.T) {
		req := logsRequest()
		got := azuremonitor.LogsExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
		}, req)
		require.NotEmpty(t, got)

		query, _ := decodePane(t, got)

		want := roundTrip(t, azclient.LogsQueryModel(testDSUID, req))
		assert.Equal(t, want, query)
	})

	t.Run("returns empty for missing required input", func(t *testing.T) {
		link := dsquery.ExploreQuery{DatasourceUID: testDSUID, DatasourceType: testDSType}

		assert.Empty(t, azuremonitor.LogsExploreURL("", link, logsRequest()))
		assert.Empty(t, azuremonitor.LogsExploreURL(testHost, dsquery.ExploreQuery{DatasourceType: testDSType}, logsRequest()))

		noQuery := logsRequest()
		noQuery.Query = "   "
		assert.Empty(t, azuremonitor.LogsExploreURL(testHost, link, noQuery))

		noWorkspace := logsRequest()
		noWorkspace.Workspace = ""
		assert.Empty(t, azuremonitor.LogsExploreURL(testHost, link, noWorkspace))
	})
}

func resourceGraphRequest() azclient.ResourceGraphRequest {
	start := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	return azclient.ResourceGraphRequest{
		Subscriptions: []string{"sub-a", "sub-b"},
		Query:         "Resources | project name, type",
		Start:         start,
		End:           start.Add(time.Hour),
	}
}

func TestResourceGraphExploreURL(t *testing.T) {
	t.Run("mirrors the resource graph query model", func(t *testing.T) {
		got := azuremonitor.ResourceGraphExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
		}, resourceGraphRequest())
		require.NotEmpty(t, got)

		query, paneRange := decodePane(t, got)
		assert.Equal(t, "Azure Resource Graph", query["queryType"])
		assert.Equal(t, "A", query["refId"])
		assertDatasourceRef(t, query)

		// Resource Graph reads a query-level "subscriptions" array, not the
		// singular "subscription" field the metrics model uses.
		assert.Equal(t, []any{"sub-a", "sub-b"}, query["subscriptions"])
		assert.NotContains(t, query, "subscription")

		arg, ok := query["azureResourceGraph"].(map[string]any)
		require.True(t, ok, "query has no azureResourceGraph block")
		assert.Equal(t, "Resources | project name, type", arg["query"])
		assert.Equal(t, "table", arg["resultFormat"])

		// The command exposes no time flags, so the pane uses the default window.
		assert.Equal(t, "now-1h", paneRange["from"])
		assert.Equal(t, "now", paneRange["to"])
	})

	t.Run("matches the query model the client sends", func(t *testing.T) {
		req := resourceGraphRequest()
		got := azuremonitor.ResourceGraphExploreURL(testHost, dsquery.ExploreQuery{
			DatasourceUID:  testDSUID,
			DatasourceType: testDSType,
		}, req)
		require.NotEmpty(t, got)

		query, _ := decodePane(t, got)

		want := roundTrip(t, azclient.ResourceGraphQueryModel(testDSUID, req))
		assert.Equal(t, want, query)
	})

	t.Run("returns empty for missing required input", func(t *testing.T) {
		link := dsquery.ExploreQuery{DatasourceUID: testDSUID, DatasourceType: testDSType}

		assert.Empty(t, azuremonitor.ResourceGraphExploreURL("", link, resourceGraphRequest()))
		assert.Empty(t, azuremonitor.ResourceGraphExploreURL(testHost, dsquery.ExploreQuery{DatasourceType: testDSType}, resourceGraphRequest()))

		noQuery := resourceGraphRequest()
		noQuery.Query = ""
		assert.Empty(t, azuremonitor.ResourceGraphExploreURL(testHost, link, noQuery))

		noSubs := resourceGraphRequest()
		noSubs.Subscriptions = nil
		assert.Empty(t, azuremonitor.ResourceGraphExploreURL(testHost, link, noSubs))
	})
}

// roundTrip encodes and decodes a query model so it compares equal to a model
// that travelled through the Explore URL.
func roundTrip(t *testing.T, model map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(model)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}
