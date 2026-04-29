package tempo_test

import (
	"net/url"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// traceQLBuilder identifies which TraceQL URL builder a table case targets.
type traceQLBuilder int

const (
	builderSearch traceQLBuilder = iota
	builderMetrics
)

func TestTraceQLExploreURLs(t *testing.T) {
	tests := []struct {
		name         string
		builder      traceQLBuilder
		host         string
		query        dsquery.ExploreQuery
		limit        int
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:    "search builds traceql search explore link",
			builder: builderSearch,
			host:    "https://ops.grafana-ops.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "grafanacloud-traces",
				DatasourceType: "tempo",
				Expr:           `{name != nil}`,
			},
			limit: 20,
			wantContains: []string{
				`"datasource":"grafanacloud-traces"`,
				`"queryType":"traceql"`,
				`"limit":20`,
				`"tableType":"traces"`,
				`"metricsQueryType":"range"`,
				`"serviceMapUseNativeHistograms":false`,
				`"query":"{name != nil}"`,
				`"from":"now-1m"`,
				`"to":"now"`,
				`"compact":false`,
			},
		},
		{
			name:    "search preserves explicit relative from",
			builder: builderSearch,
			host:    "https://ops.grafana-ops.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "grafanacloud-traces",
				DatasourceType: "tempo",
				Expr:           `{name != nil}`,
				From:           "now-1h",
			},
			limit: 20,
			wantContains: []string{
				`"from":"now-1h"`,
				`"to":"now"`,
			},
		},
		{
			name:      "search returns empty when host missing",
			builder:   builderSearch,
			host:      "",
			query:     dsquery.ExploreQuery{DatasourceUID: "tempo-uid", Expr: "{}"},
			limit:     20,
			wantEmpty: true,
		},
		{
			name:      "search returns empty when datasource UID missing",
			builder:   builderSearch,
			host:      "https://ops.grafana-ops.net",
			query:     dsquery.ExploreQuery{Expr: "{}"},
			limit:     20,
			wantEmpty: true,
		},
		{
			name:      "search returns empty when expr missing",
			builder:   builderSearch,
			host:      "https://ops.grafana-ops.net",
			query:     dsquery.ExploreQuery{DatasourceUID: "tempo-uid"},
			limit:     20,
			wantEmpty: true,
		},
		{
			name:    "search returns empty when limit is zero",
			builder: builderSearch,
			host:    "https://ops.grafana-ops.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "grafanacloud-traces",
				DatasourceType: "tempo",
				Expr:           `{name != nil}`,
			},
			limit:     0,
			wantEmpty: true,
		},
		{
			name:    "metrics builds traceql metrics explore link",
			builder: builderMetrics,
			host:    "https://ops.grafana-ops.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "grafanacloud-traces",
				DatasourceType: "tempo",
				Expr:           `{name != nil} | rate()`,
			},
			limit: 20,
			wantContains: []string{
				`"datasource":"grafanacloud-traces"`,
				`"queryType":"traceql"`,
				`"limit":20`,
				`"tableType":"traces"`,
				`"metricsQueryType":"range"`,
				`"serviceMapUseNativeHistograms":false`,
				`"query":"{name != nil} | rate()"`,
				`"from":"now-1m"`,
				`"to":"now"`,
				`"compact":false`,
			},
		},
		{
			name:    "metrics preserves explicit relative from",
			builder: builderMetrics,
			host:    "https://ops.grafana-ops.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "grafanacloud-traces",
				DatasourceType: "tempo",
				Expr:           `{name != nil} | rate()`,
				From:           "now-1h",
			},
			limit: 20,
			wantContains: []string{
				`"from":"now-1h"`,
				`"to":"now"`,
			},
		},
		{
			name:    "metrics uses instant query type for instant queries",
			builder: builderMetrics,
			host:    "https://ops.grafana-ops.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "grafanacloud-traces",
				DatasourceType: "tempo",
				Expr:           `{name != nil} | rate()`,
				Instant:        true,
			},
			limit: 20,
			wantContains: []string{
				`"metricsQueryType":"instant"`,
			},
		},
		{
			name:      "metrics returns empty when host missing",
			builder:   builderMetrics,
			host:      "",
			query:     dsquery.ExploreQuery{DatasourceUID: "tempo-uid", Expr: "{ } | rate()"},
			limit:     20,
			wantEmpty: true,
		},
		{
			name:      "metrics returns empty when datasource UID missing",
			builder:   builderMetrics,
			host:      "https://ops.grafana-ops.net",
			query:     dsquery.ExploreQuery{Expr: "{ } | rate()"},
			limit:     20,
			wantEmpty: true,
		},
		{
			name:      "metrics returns empty when expr missing",
			builder:   builderMetrics,
			host:      "https://ops.grafana-ops.net",
			query:     dsquery.ExploreQuery{DatasourceUID: "tempo-uid"},
			limit:     20,
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			switch tt.builder {
			case builderSearch:
				got = tempo.SearchExploreURL(tt.host, tt.query, tt.limit)
			case builderMetrics:
				got = tempo.MetricsExploreURL(tt.host, tt.query, tt.limit)
			default:
				t.Fatalf("unknown builder: %v", tt.builder)
			}

			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}

			require.NotEmpty(t, got)
			params := mustParseURL(t, got).Query()
			assert.Equal(t, "1", params.Get("schemaVersion"))
			panes := params.Get("panes")
			for _, want := range tt.wantContains {
				assert.Contains(t, panes, want)
			}
		})
	}
}

func TestTraceExploreURL(t *testing.T) {
	tests := []struct {
		name         string
		host         string
		query        dsquery.ExploreQuery
		traceID      string
		wantEmpty    bool
		wantOrgID    string
		wantTraceID  string
		wantContains []string
	}{
		{
			name: "builds trace explore link",
			host: "https://mystack.grafana.net/",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "tempo-uid",
				DatasourceType: "tempo",
				From:           "2026-04-24T10:00:00Z",
				To:             "2026-04-24T11:00:00Z",
				OrgID:          9,
			},
			traceID:     "287e20fb791cf30",
			wantOrgID:   "9",
			wantTraceID: "287e20fb791cf30",
			wantContains: []string{
				`"datasource":"tempo-uid"`,
				`"queryType":"traceql"`,
				`"limit":20`,
				`"tableType":"traces"`,
				`"metricsQueryType":"range"`,
				`"serviceMapUseNativeHistograms":false`,
				`"query":"287e20fb791cf30"`,
				`"from":"2026-04-24T10:00:00Z"`,
				`"to":"2026-04-24T11:00:00Z"`,
				`"compact":false`,
			},
		},
		{
			name: "preserves explicit relative from",
			host: "https://mystack.grafana.net",
			query: dsquery.ExploreQuery{
				DatasourceUID:  "tempo-uid",
				DatasourceType: "tempo",
				From:           "now-1h",
			},
			traceID:     "287e20fb791cf30",
			wantTraceID: "287e20fb791cf30",
			wantContains: []string{
				`"from":"now-1h"`,
				`"to":"now"`,
			},
		},
		{
			name:      "returns empty when host missing",
			host:      "",
			query:     dsquery.ExploreQuery{DatasourceUID: "tempo-uid"},
			traceID:   "abc",
			wantEmpty: true,
		},
		{
			name:      "returns empty when datasource UID missing",
			host:      "https://mystack.grafana.net",
			query:     dsquery.ExploreQuery{},
			traceID:   "abc",
			wantEmpty: true,
		},
		{
			name:      "returns empty when trace ID missing",
			host:      "https://mystack.grafana.net",
			query:     dsquery.ExploreQuery{DatasourceUID: "tempo-uid"},
			traceID:   "",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tempo.TraceExploreURL(tt.host, tt.query, tt.traceID)
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}

			require.NotEmpty(t, got)
			params := mustParseURL(t, got).Query()
			assert.Equal(t, "1", params.Get("schemaVersion"))
			if tt.wantOrgID != "" {
				assert.Equal(t, tt.wantOrgID, params.Get("orgId"))
			}
			if tt.wantTraceID != "" {
				assert.Equal(t, tt.wantTraceID, params.Get("traceId"))
			}
			panes := params.Get("panes")
			for _, want := range tt.wantContains {
				assert.Contains(t, panes, want)
			}
		})
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
