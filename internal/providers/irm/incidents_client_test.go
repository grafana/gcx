package irm_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *irm.IncidentClient {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := irm.NewIncidentClient(cfg)
	require.NoError(t, err)
	return c
}

// listRequest mirrors the QueryIncidentPreviews request body for assertions.
type listRequest struct {
	Query                    map[string]any  `json:"query"`
	Cursor                   *map[string]any `json:"cursor"`
	IncludeCustomFieldValues bool            `json:"includeCustomFieldValues"`
	IncludeIncidentChannels  bool            `json:"includeIncidentChannels"`
}

// previewsPage builds a one-page response with n previews named after page.
func previewsPage(page string, n int, hasMore bool, nextValue string) map[string]any {
	previews := make([]map[string]any, n)
	for i := range previews {
		previews[i] = map[string]any{"incidentID": fmt.Sprintf("inc-%s-%d", page, i), "title": "Outage " + page, "status": "active"}
	}
	return map[string]any{
		"incidentPreviews": previews,
		"cursor":           map[string]any{"hasMore": hasMore, "nextValue": nextValue},
	}
}

// datedPage builds a one-page response from incidentID → createdTime pairs.
func datedPage(created map[string]string, hasMore bool, nextValue string) map[string]any {
	previews := make([]map[string]any, 0, len(created))
	for _, id := range slices.Sorted(maps.Keys(created)) {
		previews = append(previews, map[string]any{"incidentID": id, "title": "Outage " + id, "status": "active", "createdTime": created[id]})
	}
	return map[string]any{
		"incidentPreviews": previews,
		"cursor":           map[string]any{"hasMore": hasMore, "nextValue": nextValue},
	}
}

func flexTimePtr(t time.Time) *irm.FlexTime {
	ft := irm.FlexTime(t)
	return &ft
}

// clientListCase is one List scenario: a query, a scripted server, and the
// expected outcome.
type clientListCase struct {
	name      string
	query     irm.IncidentQuery
	handler   func(t *testing.T, calls *[]listRequest) http.HandlerFunc
	wantIDs   []string
	wantLen   int
	wantCalls int
	wantErr   string
}

// runClientListCase records every request body, serves tt.handler, runs
// List, and checks the outcome against the case expectations.
func runClientListCase(t *testing.T, tt clientListCase) {
	t.Helper()
	var calls []listRequest
	inner := tt.handler(t, &calls)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req listRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		calls = append(calls, req)
		inner(w, r)
	}))
	defer server.Close()

	c := newTestClient(t, server)
	result, err := c.List(t.Context(), tt.query)

	if tt.wantErr != "" {
		require.Error(t, err)
		assert.Contains(t, err.Error(), tt.wantErr)
		return
	}

	require.NoError(t, err)
	if tt.wantIDs != nil {
		ids := make([]string, len(result))
		for i, inc := range result {
			ids[i] = inc.IncidentID
		}
		assert.Equal(t, tt.wantIDs, ids)
	} else {
		assert.Len(t, result, tt.wantLen)
	}
	assert.Len(t, calls, tt.wantCalls)
}

func TestClient_List(t *testing.T) {
	tests := []clientListCase{
		{
			name:  "returns incidents from the previews endpoint",
			query: irm.IncidentQuery{Limit: 50},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Contains(t, r.URL.Path, "/api/v1/IncidentsService.QueryIncidentPreviews")
					writeJSON(w, previewsPage("a", 2, false, ""))
				}
			},
			wantLen:   2,
			wantCalls: 1,
		},
		{
			name:  "pages with top-level cursor",
			query: irm.IncidentQuery{Limit: 50},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					switch len(*calls) {
					case 1:
						writeJSON(w, previewsPage("p1", 1, true, "cursor-1"))
					default:
						req := (*calls)[1]
						if assert.NotNil(t, req.Cursor, "second request must carry the cursor next to the query") {
							// The previously returned cursor is echoed back verbatim.
							assert.Equal(t, "cursor-1", (*req.Cursor)["nextValue"])
							assert.Equal(t, true, (*req.Cursor)["hasMore"])
						}
						writeJSON(w, previewsPage("p2", 1, false, ""))
					}
				}
			},
			wantLen:   2,
			wantCalls: 2,
		},
		{
			name:  "clamps page size to API maximum",
			query: irm.IncidentQuery{Limit: 250},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					req := (*calls)[len(*calls)-1]
					assert.LessOrEqual(t, req.Query["limit"], float64(100), "per-page limit must not exceed the documented maximum")
					switch len(*calls) {
					case 1:
						writeJSON(w, previewsPage("p1", 100, true, "cursor-1"))
					case 2:
						// 150 remain wanted; the page request must ask for 100.
						assert.InDelta(t, 100, req.Query["limit"], 0)
						writeJSON(w, previewsPage("p2", 100, true, "cursor-2"))
					default:
						// 50 remain wanted; the page request must shrink.
						assert.InDelta(t, 50, req.Query["limit"], 0)
						writeJSON(w, previewsPage("p3", 50, false, ""))
					}
				}
			},
			wantLen:   250,
			wantCalls: 3,
		},
		{
			name:  "stops at limit when the server has more",
			query: irm.IncidentQuery{Limit: 1},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, previewsPage("p1", 1, true, "cursor-1"))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "truncates when the server over-returns",
			query: irm.IncidentQuery{Limit: 3},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, previewsPage("p1", 5, false, ""))
				}
			},
			wantLen:   3,
			wantCalls: 1,
		},
		{
			name:  "stops on hasMore with empty cursor value",
			query: irm.IncidentQuery{Limit: 50},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					// A misbehaving server claims more pages but provides no
					// cursor to fetch them; the client must not loop forever.
					writeJSON(w, previewsPage("p1", 1, true, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "limit zero defaults to one full page",
			query: irm.IncidentQuery{},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					req := (*calls)[0]
					assert.InDelta(t, 100, req.Query["limit"], 0)
					writeJSON(w, previewsPage("p1", 100, true, "cursor-1"))
				}
			},
			wantLen:   100,
			wantCalls: 1,
		},
		{
			name:  "propagates error",
			query: irm.IncidentQuery{Limit: 10},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					writeJSON(w, map[string]string{"error": "internal error"})
				}
			},
			wantErr: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runClientListCase(t, tt) })
	}
}

// TestClient_List_LabelFilters covers client-side label matching: keyed vs
// legacy labels, labels carried in `value` rather than `label`, double quotes,
// and paging until enough matches accumulate.
func TestClient_List_LabelFilters(t *testing.T) {
	tests := []clientListCase{
		{
			name:  "matches keyed and legacy labels client-side",
			query: irm.IncidentQuery{Limit: 10, IncidentLabels: []string{"squad:mimir"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					req := (*calls)[0]
					assert.NotContains(t, req.Query, "queryString")
					assert.NotContains(t, req.Query, "incidentLabels")
					assert.Equal(t, "DESC", req.Query["orderDirection"])
					assert.True(t, req.IncludeCustomFieldValues)
					assert.True(t, req.IncludeIncidentChannels)
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{
								"incidentID": "inc-keyed",
								"title":      "Keyed squad",
								"status":     "active",
								"labels":     []map[string]any{{"key": "squad", "label": "mimir"}},
							},
							{
								"incidentID": "inc-legacy",
								"title":      "Legacy tag",
								"status":     "active",
								"labels":     []map[string]any{{"key": "Tags", "label": "squad:mimir"}},
							},
							{
								"incidentID": "inc-other",
								"title":      "Other squad",
								"status":     "active",
								"labels":     []map[string]any{{"key": "squad", "label": "tempo"}},
							},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-keyed", "inc-legacy"},
			wantCalls: 1,
		},
		{
			// QueryIncidentPreviews can carry a label's text in `value`
			// rather than `label`; incidentLabelValue falls back to it, so
			// value-only labels must match both key:value filters (keyed
			// branch) and bare label-text filters (direct branch).
			name:  "matches labels carrying value instead of label",
			query: irm.IncidentQuery{Limit: 10, IncidentLabels: []string{"squad:mimir", "security"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.NotContains(t, (*calls)[0].Query, "queryString")
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{
								"incidentID": "inc-value",
								"title":      "Value-only labels",
								"status":     "active",
								"labels": []map[string]any{
									{"key": "squad", "value": "mimir"},
									{"key": "Tags", "value": "security"},
								},
							},
							{
								"incidentID": "inc-partial",
								"title":      "Missing security",
								"status":     "active",
								"labels":     []map[string]any{{"key": "squad", "value": "mimir"}},
							},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-value"},
			wantCalls: 1,
		},
		{
			name:  "labels with double quotes are matched client-side",
			query: irm.IncidentQuery{Limit: 10, IncidentLabels: []string{`the "big" outage`}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.NotContains(t, (*calls)[0].Query, "queryString")
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{
								"incidentID": "inc-quoted",
								"title":      "Quoted label",
								"status":     "active",
								"labels":     []map[string]any{{"key": "Tags", "label": `the "big" outage`}},
							},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-quoted"},
			wantCalls: 1,
		},
		{
			name:  "keeps paging until enough client-side label matches are found",
			query: irm.IncidentQuery{Limit: 1, IncidentLabels: []string{"component:warpstream"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					req := (*calls)[len(*calls)-1]
					assert.InDelta(t, 100, req.Query["limit"], 0)
					switch len(*calls) {
					case 1:
						writeJSON(w, map[string]any{
							"incidentPreviews": []map[string]any{
								{
									"incidentID": "inc-nonmatching",
									"title":      "Other incident",
									"status":     "active",
									"labels":     []map[string]any{{"key": "component", "label": "database"}},
								},
							},
							"cursor": map[string]any{"hasMore": true, "nextValue": "cursor-1"},
						})
					default:
						writeJSON(w, map[string]any{
							"incidentPreviews": []map[string]any{
								{
									"incidentID": "inc-warpstream",
									"title":      "WarpStream incident",
									"status":     "active",
									"labels":     []map[string]any{{"key": "component", "label": "warpstream"}},
								},
							},
							"cursor": map[string]any{"hasMore": false},
						})
					}
				}
			},
			wantIDs:   []string{"inc-warpstream"},
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runClientListCase(t, tt) })
	}
}

// TestClient_List_StatusFilters covers the status filter compiling into
// query-string terms: a single bare status, multiple statuses ORed together,
// and rejection of a status outside the supported enum.
func TestClient_List_StatusFilters(t *testing.T) {
	tests := []clientListCase{
		{
			name:  "single status becomes a bare status term",
			query: irm.IncidentQuery{Limit: 10, Statuses: []string{"active"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.Equal(t, "status:active", (*calls)[0].Query["queryString"])
					writeJSON(w, previewsPage("p1", 1, false, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "multiple statuses are ORed, not ANDed",
			query: irm.IncidentQuery{Limit: 10, Statuses: []string{"active", "resolved"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					// Juxtaposition would AND the statuses and match nothing.
					assert.Equal(t, "or(status:active status:resolved)", (*calls)[0].Query["queryString"])
					writeJSON(w, previewsPage("p1", 1, false, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "rejects status outside the supported enum",
			query: irm.IncidentQuery{Limit: 10, Statuses: []string{`active status:resolved`}},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(_ http.ResponseWriter, _ *http.Request) {
					t.Error("API must not be called for an invalid status")
				}
			},
			wantErr: "must be active or resolved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runClientListCase(t, tt) })
	}
}

// TestClient_List_CombinedAndSeverityFilters covers filters composing into one
// query string: labels, status and severity ANDed together, a raw query string
// overriding the structured filters, and rejection of an inexpressible severity.
func TestClient_List_CombinedAndSeverityFilters(t *testing.T) {
	tests := []clientListCase{
		{
			name:  "labels, status and severity AND together",
			query: irm.IncidentQuery{Limit: 10, IncidentLabels: []string{"security"}, Statuses: []string{"active"}, Severity: "major"},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.Equal(t, `status:active severity:"major"`, (*calls)[0].Query["queryString"])
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{
								"incidentID":    "inc-security",
								"title":         "Security incident",
								"status":        "active",
								"severityLabel": "Major",
								"labels":        []map[string]any{{"key": "Tags", "label": "security"}},
							},
							{
								"incidentID":    "inc-other",
								"title":         "Other incident",
								"status":        "active",
								"severityLabel": "Major",
								"labels":        []map[string]any{{"key": "Tags", "label": "other"}},
							},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-security"},
			wantCalls: 1,
		},
		{
			name:  "raw query string is used verbatim and overrides structured filters",
			query: irm.IncidentQuery{Limit: 10, QueryString: "isdrill:true", IncidentLabels: []string{"ignored"}, Statuses: []string{"active"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.Equal(t, "isdrill:true", (*calls)[0].Query["queryString"])
					writeJSON(w, previewsPage("p1", 1, false, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "raw query string skips structured filter validation",
			query: irm.IncidentQuery{Limit: 10, QueryString: "isdrill:true", Statuses: []string{"not-a-status"}, Severity: `the "big" sev`},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.Equal(t, "isdrill:true", (*calls)[0].Query["queryString"])
					writeJSON(w, previewsPage("p1", 1, false, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "rejects severity containing a double quote",
			query: irm.IncidentQuery{Limit: 10, Severity: `the "big" sev`},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(_ http.ResponseWriter, _ *http.Request) {
					t.Error("API must not be called for an inexpressible severity")
				}
			},
			wantErr: "cannot express values containing double quotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runClientListCase(t, tt) })
	}
}

// TestClient_List_DateFiltersAndErrors covers the client-side date window (the
// endpoint has no date fields): full-page fetches, the from/to bounds, missing
// createdTime, early stop under newest-first order — plus surfacing an in-band
// response error.
func TestClient_List_DateFiltersAndErrors(t *testing.T) {
	tests := []clientListCase{
		{
			name: "fetches full pages while date filtering",
			query: irm.IncidentQuery{
				Limit:  2,
				DateTo: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)),
			},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					// Client-side filtering can discard any number of
					// previews, so the page request must not shrink to the
					// remaining limit.
					req := (*calls)[0]
					assert.InDelta(t, 100, req.Query["limit"], 0)
					writeJSON(w, datedPage(map[string]string{
						"inc-1": "2026-06-11T10:00:00Z",
						"inc-2": "2026-06-09T09:00:00Z",
						"inc-3": "2026-06-08T08:00:00Z",
					}, false, ""))
				}
			},
			wantIDs:   []string{"inc-2", "inc-3"},
			wantCalls: 1,
		},
		{
			name: "applies the from bound client-side and stops early",
			query: irm.IncidentQuery{
				Limit:    50,
				DateFrom: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)),
			},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					// No date fields exist on the wire; the bound must be
					// enforced client-side. Descending createdTime order
					// means everything past inc-2 is older still, so the
					// client must filter and stop after one call.
					req := (*calls)[0]
					assert.NotContains(t, req.Query, "dateFrom")
					writeJSON(w, datedPage(map[string]string{
						"inc-1": "2026-06-11T10:00:00Z",
						"inc-2": "2026-06-10T09:00:00Z",
						"inc-3": "2026-06-09T08:00:00Z",
					}, true, "cursor-1"))
				}
			},
			wantIDs:   []string{"inc-1", "inc-2"},
			wantCalls: 1,
		},
		{
			name: "excludes previews without a createdTime when a bound is set",
			query: irm.IncidentQuery{
				Limit:    50,
				DateFrom: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)),
			},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					// inc-2 has no createdTime and cannot be placed in the
					// window, so it must not leak into the results.
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{"incidentID": "inc-1", "title": "Outage", "status": "active", "createdTime": "2026-06-11T10:00:00Z"},
							{"incidentID": "inc-2", "title": "No created time", "status": "active"},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-1"},
			wantCalls: 1,
		},
		{
			name: "applies the to bound client-side and keeps paging",
			query: irm.IncidentQuery{
				Limit:  50,
				DateTo: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)),
			},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					// Page one is entirely newer than the to bound (exclusive);
					// the matching incidents are on page two.
					if len(*calls) == 1 {
						writeJSON(w, datedPage(map[string]string{
							"inc-1": "2026-06-11T10:00:00Z",
							"inc-2": "2026-06-10T00:00:00Z",
						}, true, "cursor-1"))
					} else {
						writeJSON(w, datedPage(map[string]string{
							"inc-3": "2026-06-09T08:00:00Z",
						}, false, ""))
					}
				}
			},
			wantIDs:   []string{"inc-3"},
			wantCalls: 2,
		},
		{
			name:  "surfaces in-band response error",
			query: irm.IncidentQuery{Limit: 10},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, map[string]any{"incidentPreviews": []map[string]any{}, "cursor": map[string]any{}, "error": "Invalid query string"})
				}
			},
			wantErr: "Invalid query string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runClientListCase(t, tt) })
	}
}

// TestClient_List_MapsSeverityLabel pins the preview→Incident conversion:
// previews carry severityLabel where full incidents carried severity.
func TestClient_List_MapsSeverityLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"incidentPreviews": []map[string]any{
				{"incidentID": "inc-1", "title": "Outage", "status": "active", "severityLabel": "Major", "severityID": "sev-2"},
			},
			"cursor": map[string]any{"hasMore": false},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	result, err := c.List(t.Context(), irm.IncidentQuery{Limit: 10})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Major", result[0].Severity)
	assert.Equal(t, "sev-2", result[0].SeverityID)
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "returns incident by ID",
			id:   "inc-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "IncidentsService.GetIncident")
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.Equal(t, "inc-123", body["incidentID"])
				writeJSON(w, map[string]any{
					"incident": map[string]any{"incidentID": "inc-123", "title": "Test", "status": "active"},
				})
			},
			wantID: "inc-123",
		},
		{
			name: "returns not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.Get(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, result.IncidentID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "IncidentsService.CreateIncident")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "DB Outage", body["title"])
		writeJSON(w, map[string]any{
			"incident": map[string]any{"incidentID": "new-123", "title": "DB Outage", "status": "active"},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	inc, err := c.Create(t.Context(), &irm.Incident{
		Title:  "DB Outage",
		Status: "active",
	})
	require.NoError(t, err)
	assert.Equal(t, "new-123", inc.IncidentID)
	assert.Equal(t, "DB Outage", inc.Title)
}

func TestClient_UpdateStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "IncidentsService.UpdateStatus")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "inc-456", body["incidentID"])
		assert.Equal(t, "resolved", body["status"])
		writeJSON(w, map[string]any{
			"incident": map[string]any{"incidentID": "inc-456", "title": "Resolved", "status": "resolved"},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	inc, err := c.UpdateStatus(t.Context(), "inc-456", "resolved")
	require.NoError(t, err)
	assert.Equal(t, "resolved", inc.Status)
}

func TestClient_QueryActivity(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		limit   int
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name:  "returns activity items",
			id:    "inc-123",
			limit: 10,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "ActivityService.QueryActivity")
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				query, _ := body["query"].(map[string]any)
				assert.Equal(t, "inc-123", query["incidentID"])
				writeJSON(w, map[string]any{
					"activityItems": []map[string]any{
						{"activityItemID": "act-1", "incidentID": "inc-123", "activityKind": "userNote", "body": "First note"},
						{"activityItemID": "act-2", "incidentID": "inc-123", "activityKind": "statusChange", "body": "Status changed"},
					},
				})
			},
			wantLen: 2,
		},
		{
			name:  "returns empty list",
			id:    "inc-empty",
			limit: 50,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"activityItems": []map[string]any{}})
			},
			wantLen: 0,
		},
		{
			name:  "propagates error",
			id:    "inc-err",
			limit: 10,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			items, err := c.QueryActivity(t.Context(), tt.id, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, items, tt.wantLen)
		})
	}
}

func TestClient_QueryIncidentContext(t *testing.T) {
	alertGroupID := "ag-42"

	tests := []struct {
		name    string
		query   irm.IncidentContextQuery
		handler http.HandlerFunc
		wantLen int
		wantErr string
	}{
		{
			name: "returns contexts and forwards filters",
			query: irm.IncidentContextQuery{
				IncidentID:   "inc-123",
				Type:         "genericURL",
				Status:       "active",
				AlertGroupID: alertGroupID,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "IncidentContextService.QueryIncidentContext")
				assert.NotContains(t, r.URL.Path, "/v1/", "IncidentContextService 404s under the v1 prefix")
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				query, _ := body["query"].(map[string]any)
				assert.Equal(t, "inc-123", query["incidentID"])
				assert.Equal(t, "genericURL", query["type"])
				assert.Equal(t, "active", query["status"])
				assert.Equal(t, alertGroupID, query["alertGroupID"])
				writeJSON(w, map[string]any{
					"incidentContexts": []map[string]any{
						{"contextID": "ctx-1", "incidentID": "inc-123", "type": "genericURL", "alertGroupID": alertGroupID},
						{"contextID": "ctx-2", "incidentID": "inc-123", "type": "grafana.dashboard"},
					},
				})
			},
			wantLen: 2,
		},
		{
			name:  "returns empty list",
			query: irm.IncidentContextQuery{IncidentID: "inc-empty"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"incidentContexts": []map[string]any{}})
			},
			wantLen: 0,
		},
		{
			name:    "missing incident ID is rejected client-side",
			query:   irm.IncidentContextQuery{},
			handler: func(_ http.ResponseWriter, _ *http.Request) { t.Fatal("server should not be hit") },
			wantErr: "incidentID is required",
		},
		{
			name:  "propagates server error",
			query: irm.IncidentContextQuery{IncidentID: "inc-err"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal error"})
			},
			wantErr: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			contexts, err := c.QueryIncidentContext(t.Context(), tt.query)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Len(t, contexts, tt.wantLen)
		})
	}
}

func TestClient_AddActivity(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		body    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "adds activity note",
			id:   "inc-123",
			body: "This is a note",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "ActivityService.AddActivity")
				var reqBody map[string]string
				_ = json.NewDecoder(r.Body).Decode(&reqBody)
				assert.Equal(t, "inc-123", reqBody["incidentID"])
				assert.Equal(t, "This is a note", reqBody["body"])
				assert.Equal(t, "userNote", reqBody["activityKind"])
				writeJSON(w, map[string]any{})
			},
			wantErr: false,
		},
		{
			name: "propagates error",
			id:   "inc-err",
			body: "note",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, map[string]string{"error": "forbidden"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			err := c.AddActivity(t.Context(), tt.id, tt.body)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClient_GetSeverities(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns severities",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "SeveritiesService.GetOrgSeverities")
				assert.NotContains(t, r.URL.Path, "/v1/", "SeveritiesService 404s under the v1 prefix")
				writeJSON(w, map[string]any{
					"severities": []map[string]any{
						{"severityID": "sev-1", "displayLabel": "Critical", "level": 1, "color": "#FF0000"},
						{"severityID": "sev-2", "displayLabel": "High", "level": 2, "color": "#FF8800"},
						{"severityID": "sev-3", "displayLabel": "Low", "level": 3},
					},
				})
			},
			wantLen: 3,
		},
		{
			name: "returns empty list",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"severities": []map[string]any{}})
			},
			wantLen: 0,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			sevs, err := c.GetSeverities(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, sevs, tt.wantLen)
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
