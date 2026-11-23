package kg_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestSummarizeEntityCounts(t *testing.T) {
	tests := []struct {
		name   string
		counts map[string]int64
		want   kg.EntitiesSummary
	}{
		{
			name:   "no entities",
			counts: map[string]int64{},
			want:   kg.EntitiesSummary{ByType: map[string]int64{}},
		},
		{
			name:   "observed entities only",
			counts: map[string]int64{"Service": 3, "Pod": 7},
			want:   kg.EntitiesSummary{Total: 10, ByType: map[string]int64{"Service": 3, "Pod": 7}},
		},
		{
			name: "catalog records are split out of the total",
			counts: map[string]int64{
				"Service": 3, "Pod": 7,
				"Account": 2, "EntityType": 3, "Env": 4, "Schema": 5,
			},
			want: kg.EntitiesSummary{
				Total:  10,
				ByType: map[string]int64{"Service": 3, "Pod": 7},
				Internal: &kg.EntityCount{
					Total:  14,
					ByType: map[string]int64{"Account": 2, "EntityType": 3, "Env": 4, "Schema": 5},
				},
			},
		},
		{
			name:   "only catalog records leaves an empty observed total",
			counts: map[string]int64{"Account": 2, "EntityType": 3, "Schema": 5},
			want: kg.EntitiesSummary{
				ByType: map[string]int64{},
				Internal: &kg.EntityCount{
					Total:  10,
					ByType: map[string]int64{"Account": 2, "EntityType": 3, "Schema": 5},
				},
			},
		},
		{
			name:   "zero counts are dropped",
			counts: map[string]int64{"Service": 0, "Pod": 2, "Schema": 0},
			want:   kg.EntitiesSummary{Total: 2, ByType: map[string]int64{"Pod": 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, kg.SummarizeEntityCounts(tt.counts))
		})
	}
}

func TestEntitiesSummary_JSONOmitsEmptyInternal(t *testing.T) {
	tests := []struct {
		name   string
		counts map[string]int64
		want   string
	}{
		{
			name:   "no catalog records",
			counts: map[string]int64{"Pod": 2},
			want:   `{"total":2,"byType":{"Pod":2}}`,
		},
		{
			name:   "with catalog records",
			counts: map[string]int64{"Pod": 2, "Schema": 5},
			want:   `{"total":2,"byType":{"Pod":2},"internal":{"total":5,"byType":{"Schema":5}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(kg.SummarizeEntityCounts(tt.counts))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(b))
		})
	}
}

func insightResult(insights ...any) kg.SearchResult {
	return kg.SearchResult{Assertion: map[string]any{"assertions": insights}}
}

func TestSummarizeInsights(t *testing.T) {
	tests := []struct {
		name    string
		results []kg.SearchResult
		want    kg.InsightsSummary
	}{
		{
			name:    "no insights",
			results: []kg.SearchResult{{}, insightResult()},
			want: kg.InsightsSummary{
				ByCategory: map[string]kg.InsightGroup{},
				BySeverity: map[string]int{},
				ByName:     map[string]int{},
			},
		},
		{
			name: "grouped by category with per-category names",
			results: []kg.SearchResult{
				insightResult(
					map[string]any{"assertionName": "ErrorRatioBreach", "category": "error", "severity": "critical"},
					map[string]any{"assertionName": "InsightB", "category": "amend", "severity": "info"},
				),
				insightResult(
					map[string]any{"assertionName": "ErrorRatioBreach", "category": "error", "severity": "critical"},
					map[string]any{"assertionName": "InsightC", "category": "anomaly", "severity": "warning"},
				),
			},
			want: kg.InsightsSummary{
				Total: 4,
				ByCategory: map[string]kg.InsightGroup{
					"error":   {Total: 2, ByName: map[string]int{"ErrorRatioBreach": 2}},
					"amend":   {Total: 1, ByName: map[string]int{"InsightB": 1}},
					"anomaly": {Total: 1, ByName: map[string]int{"InsightC": 1}},
				},
				BySeverity: map[string]int{"CRITICAL": 2, "INFO": 1, "WARNING": 1},
				ByName:     map[string]int{"ErrorRatioBreach": 2, "InsightB": 1, "InsightC": 1},
			},
		},
		{
			name: "category case is normalised and missing fields count as unknown",
			results: []kg.SearchResult{
				insightResult(
					map[string]any{"assertionName": "Saturated", "category": "Saturation", "severity": "warning"},
					map[string]any{},
					"not an insight",
				),
			},
			want: kg.InsightsSummary{
				Total: 2,
				ByCategory: map[string]kg.InsightGroup{
					"saturation": {Total: 1, ByName: map[string]int{"Saturated": 1}},
					"unknown":    {Total: 1, ByName: map[string]int{"UNKNOWN": 1}},
				},
				BySeverity: map[string]int{"WARNING": 1, "UNKNOWN": 1},
				ByName:     map[string]int{"Saturated": 1, "UNKNOWN": 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, kg.SummarizeInsights(tt.results))
		})
	}
}

// statsPage is one canned answer of the fake search endpoint.
type statsPage struct {
	entities []map[string]any
	last     bool
	limitHit bool
	status   int // non-zero: answer with this HTTP status instead
}

func statsEntity(typ, name string, insights ...map[string]any) map[string]any {
	list := make([]any, len(insights))
	for i, a := range insights {
		list[i] = a
	}
	return map[string]any{
		"name": name, "type": typ, "scope": map[string]any{"env": "prod"},
		"assertion": map[string]any{"assertions": list},
	}
}

func statsInsight(name, category string) map[string]any {
	return map[string]any{"assertionName": name, "category": category, "severity": "warning"}
}

func TestStatsCommand(t *testing.T) {
	const prefix = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1"
	counts := map[string]int64{"Service": 3, "Pod": 2, "Account": 2, "Schema": 5}
	errInsight := statsInsight("ErrorRatioBreach", "error")
	amend := statsInsight("InsightB", "amend")
	podPage := statsPage{entities: []map[string]any{statsEntity("Pod", "pod-1", errInsight)}, last: true}

	tests := []struct {
		name           string
		args           []string
		maxPages       int
		pages          map[string][]statsPage
		wantSearched   []string
		wantInsights   float64
		wantByCategory map[string]float64
		wantIncomplete []any
		wantStderr     string
	}{
		{
			name: "follows pages, counts a repeated row once, counts insights on catalog records",
			pages: map[string][]statsPage{
				"Service": {
					{entities: []map[string]any{statsEntity("Service", "svc-a", errInsight), statsEntity("Service", "svc-b", amend)}},
					{entities: []map[string]any{statsEntity("Service", "svc-b", amend), statsEntity("Service", "svc-c", errInsight)}, last: true},
				},
				"Pod":     {podPage},
				"Account": {{entities: []map[string]any{statsEntity("Account", "tenant", statsInsight("InsightD", "failure"))}, last: true}},
			},
			wantSearched:   []string{"Account", "Pod", "Schema", "Service"},
			wantInsights:   5,
			wantByCategory: map[string]float64{"error": 3, "amend": 1, "failure": 1},
		},
		{
			name:     "a type past the page bound is listed as incomplete",
			maxPages: 2,
			pages: map[string][]statsPage{
				"Service": {
					{entities: []map[string]any{statsEntity("Service", "svc-a", errInsight)}},
					{entities: []map[string]any{statsEntity("Service", "svc-b", errInsight)}},
					{entities: []map[string]any{statsEntity("Service", "svc-c", errInsight)}, last: true},
				},
				"Pod": {podPage},
			},
			wantSearched:   []string{"Account", "Pod", "Schema", "Service"},
			wantInsights:   3,
			wantByCategory: map[string]float64{"error": 3},
			wantIncomplete: []any{"Service"},
		},
		{
			name: "a server error on a later page keeps the earlier rows and marks the type",
			pages: map[string][]statsPage{
				"Service": {
					{entities: []map[string]any{statsEntity("Service", "svc-a", errInsight)}},
					{status: http.StatusInternalServerError},
				},
				"Pod": {podPage},
			},
			wantSearched:   []string{"Account", "Pod", "Schema", "Service"},
			wantInsights:   2,
			wantByCategory: map[string]float64{"error": 2},
			wantIncomplete: []any{"Service"},
			wantStderr:     `entity type "Service": page 1 failed`,
		},
		{
			name: "a server-side result limit marks the type",
			pages: map[string][]statsPage{
				"Service": {{entities: []map[string]any{statsEntity("Service", "svc-a", amend)}, last: true, limitHit: true}},
				"Pod":     {podPage},
			},
			wantSearched:   []string{"Account", "Pod", "Schema", "Service"},
			wantInsights:   2,
			wantByCategory: map[string]float64{"amend": 1, "error": 1},
			wantIncomplete: []any{"Service"},
		},
		{
			name: "--type limits the insight search but not the entity counts",
			args: []string{"--type", "Pod"},
			pages: map[string][]statsPage{
				"Pod": {podPage},
			},
			wantSearched:   []string{"Pod"},
			wantInsights:   1,
			wantByCategory: map[string]float64{"error": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			if tt.maxPages > 0 {
				defer kg.SetStatsMaxSearchPages(tt.maxPages)()
			}
			var mu sync.Mutex
			searched := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case prefix + "/entity_type/count":
					_ = json.NewEncoder(w).Encode(counts)
				case prefix + "/search":
					var req struct {
						FilterCriteria []struct {
							EntityType string `json:"entityType"`
						} `json:"filterCriteria"`
						PageNum int `json:"pageNum"`
					}
					if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) || !assert.Len(t, req.FilterCriteria, 1) {
						return
					}
					et := req.FilterCriteria[0].EntityType
					mu.Lock()
					searched[et] = true
					mu.Unlock()
					pages := tt.pages[et]
					if req.PageNum >= len(pages) {
						_, _ = fmt.Fprint(w, `{"data":{"entities":[],"lastPage":true}}`)
						return
					}
					p := pages[req.PageNum]
					if p.status != 0 {
						w.WriteHeader(p.status)
						_, _ = fmt.Fprint(w, `{"message":"boom"}`)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
						"entities": p.entities, "pageNum": req.PageNum, "lastPage": p.last, "searchResultsMaxLimitHit": p.limitHit,
					}})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"--from", "1757000000", "--to", "1757003600"}, tt.args...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewSummaryCommand(loader) }, args, "")
			require.NoError(t, err)
			if tt.wantStderr != "" {
				assert.Contains(t, stderr, tt.wantStderr)
			}

			var got struct {
				Entities struct {
					Total    float64            `json:"total"`
					ByType   map[string]float64 `json:"byType"`
					Internal struct {
						Total float64 `json:"total"`
					} `json:"internal"`
				} `json:"entities"`
				Insights struct {
					Total      float64 `json:"total"`
					ByCategory map[string]struct {
						Total float64 `json:"total"`
					} `json:"byCategory"`
					IncompleteTypes []any `json:"incompleteTypes"`
				} `json:"insights"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &got), out)
			assert.InDelta(t, 5, got.Entities.Total, 0, "entities.total counts observed types only, whatever --type is")
			assert.Equal(t, map[string]float64{"Service": 3, "Pod": 2}, got.Entities.ByType)
			assert.InDelta(t, 7, got.Entities.Internal.Total, 0)
			assert.InDelta(t, tt.wantInsights, got.Insights.Total, 0)
			gotCats := map[string]float64{}
			for k, v := range got.Insights.ByCategory {
				gotCats[k] = v.Total
			}
			assert.Equal(t, tt.wantByCategory, gotCats)
			assert.Equal(t, tt.wantIncomplete, got.Insights.IncompleteTypes)
			gotSearched := make([]string, 0, len(searched))
			for et := range searched {
				gotSearched = append(gotSearched, et)
			}
			sort.Strings(gotSearched)
			assert.Equal(t, tt.wantSearched, gotSearched)
		})
	}
}
