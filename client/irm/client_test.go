package irm_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	irm "github.com/grafana/gcx/client/irm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(server *httptest.Server) *irm.IncidentClient {
	return irm.NewIncidentClient(server.Client(), server.URL)
}

// writeJSON encodes v as JSON to w.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func flexTimePtr(t time.Time) *irm.FlexTime {
	ft := irm.FlexTime(t)
	return &ft
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

type listCase struct {
	name      string
	query     irm.IncidentQuery
	handler   func(t *testing.T, calls *[]listRequest) http.HandlerFunc
	wantIDs   []string
	wantLen   int
	wantCalls int
	wantErr   string
}

func runListCase(t *testing.T, tt listCase) {
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

	result, err := newTestClient(server).List(t.Context(), tt.query)

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
	tests := []listCase{
		{
			name:  "returns incidents from the previews endpoint",
			query: irm.IncidentQuery{Limit: 50},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.QueryIncidentPreviews", r.URL.Path)
					writeJSON(w, previewsPage("a", 2, false, ""))
				}
			},
			wantLen:   2,
			wantCalls: 1,
		},
		{
			name:  "pages with the returned cursor",
			query: irm.IncidentQuery{Limit: 50},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					if len(*calls) == 1 {
						writeJSON(w, previewsPage("p1", 1, true, "cursor-1"))
						return
					}
					req := (*calls)[1]
					if assert.NotNil(t, req.Cursor, "second request must carry the cursor next to the query") {
						assert.Equal(t, "cursor-1", (*req.Cursor)["nextValue"])
					}
					writeJSON(w, previewsPage("p2", 1, false, ""))
				}
			},
			wantLen:   2,
			wantCalls: 2,
		},
		{
			name:  "limit zero defaults to one full page",
			query: irm.IncidentQuery{},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.InDelta(t, 100, (*calls)[0].Query["limit"], 0)
					assert.Equal(t, "DESC", (*calls)[0].Query["orderDirection"])
					assert.True(t, (*calls)[0].IncludeCustomFieldValues)
					assert.True(t, (*calls)[0].IncludeIncidentChannels)
					writeJSON(w, previewsPage("p1", 100, true, "cursor-1"))
				}
			},
			wantLen:   100,
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
					writeJSON(w, previewsPage("p1", 1, true, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "maps severityLabel onto Severity",
			query: irm.IncidentQuery{Limit: 10},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{"incidentID": "inc-1", "title": "Outage", "status": "active", "severityLabel": "Major", "severityID": "sev-2"},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-1"},
			wantCalls: 1,
		},
		{
			name:  "propagates a transport error",
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
		{
			name:  "surfaces an in-band response error",
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
		t.Run(tt.name, func(t *testing.T) { runListCase(t, tt) })
	}
}

// TestClient_List_Filters covers the filters the endpoint has no fields for:
// statuses and severity compile into the query-string language, labels and
// the date window are matched client-side.
func TestClient_List_Filters(t *testing.T) {
	tests := []listCase{
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
			query: irm.IncidentQuery{Limit: 10, Statuses: []string{"active", "resolved"}, Severity: "major"},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.Equal(t, `or(status:active status:resolved) severity:"major"`, (*calls)[0].Query["queryString"])
					writeJSON(w, previewsPage("p1", 1, false, ""))
				}
			},
			wantLen:   1,
			wantCalls: 1,
		},
		{
			name:  "raw query string is used verbatim and skips validation",
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
			name:  "rejects a status outside the supported enum",
			query: irm.IncidentQuery{Limit: 10, Statuses: []string{"active status:resolved"}},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(_ http.ResponseWriter, _ *http.Request) {
					t.Error("API must not be called for an invalid status")
				}
			},
			wantErr: "must be active or resolved",
		},
		{
			name:  "rejects a severity containing a double quote",
			query: irm.IncidentQuery{Limit: 10, Severity: `the "big" sev`},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(_ http.ResponseWriter, _ *http.Request) {
					t.Error("API must not be called for an inexpressible severity")
				}
			},
			wantErr: "cannot express values containing double quotes",
		},
		{
			name:  "matches keyed and legacy labels client-side",
			query: irm.IncidentQuery{Limit: 10, IncidentLabels: []string{"squad:mimir"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.NotContains(t, (*calls)[0].Query, "queryString")
					assert.InDelta(t, 100, (*calls)[0].Query["limit"], 0, "client-side filtering must fetch full pages")
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{"incidentID": "inc-keyed", "status": "active", "labels": []map[string]any{{"key": "squad", "label": "mimir"}}},
							{"incidentID": "inc-legacy", "status": "active", "labels": []map[string]any{{"key": "Tags", "label": "squad:mimir"}}},
							{"incidentID": "inc-value", "status": "active", "labels": []map[string]any{{"key": "squad", "value": "mimir"}}},
							{"incidentID": "inc-other", "status": "active", "labels": []map[string]any{{"key": "squad", "label": "tempo"}}},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-keyed", "inc-legacy", "inc-value"},
			wantCalls: 1,
		},
		{
			name:  "keeps paging until enough label matches are found",
			query: irm.IncidentQuery{Limit: 1, IncidentLabels: []string{"component:warpstream"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					if len(*calls) == 1 {
						writeJSON(w, map[string]any{
							"incidentPreviews": []map[string]any{
								{"incidentID": "inc-nonmatching", "status": "active", "labels": []map[string]any{{"key": "component", "label": "database"}}},
							},
							"cursor": map[string]any{"hasMore": true, "nextValue": "cursor-1"},
						})
						return
					}
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{"incidentID": "inc-warpstream", "status": "active", "labels": []map[string]any{{"key": "component", "label": "warpstream"}}},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-warpstream"},
			wantCalls: 2,
		},
		{
			name:  "applies the from bound and stops early under newest-first order",
			query: irm.IncidentQuery{Limit: 50, DateFrom: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					assert.NotContains(t, (*calls)[0].Query, "dateFrom", "the endpoint has no date fields")
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
			name:  "applies the exclusive to bound and keeps paging",
			query: irm.IncidentQuery{Limit: 50, DateTo: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					if len(*calls) == 1 {
						writeJSON(w, datedPage(map[string]string{
							"inc-1": "2026-06-11T10:00:00Z",
							"inc-2": "2026-06-10T00:00:00Z",
						}, true, "cursor-1"))
						return
					}
					writeJSON(w, datedPage(map[string]string{"inc-3": "2026-06-09T08:00:00Z"}, false, ""))
				}
			},
			wantIDs:   []string{"inc-3"},
			wantCalls: 2,
		},
		{
			name:  "excludes previews without a createdTime when a bound is set",
			query: irm.IncidentQuery{Limit: 50, DateFrom: flexTimePtr(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, map[string]any{
						"incidentPreviews": []map[string]any{
							{"incidentID": "inc-1", "status": "active", "createdTime": "2026-06-11T10:00:00Z"},
							{"incidentID": "inc-2", "status": "active"},
						},
						"cursor": map[string]any{"hasMore": false},
					})
				}
			},
			wantIDs:   []string{"inc-1"},
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runListCase(t, tt) })
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		handler      http.HandlerFunc
		wantID       string
		wantPaths    []string
		wantNotFound bool
		wantErr      string
	}{
		{
			name: "returns the incident by ID",
			id:   "inc-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.Equal(t, "inc-123", body["incidentID"])
				writeJSON(w, map[string]any{
					"incident": map[string]any{"incidentID": "inc-123", "title": "Test", "status": "active"},
				})
			},
			wantID:    "inc-123",
			wantPaths: []string{"/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.GetIncident"},
		},
		{
			name: "retries the unversioned base path on 404",
			id:   "inc-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.GetIncident" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				writeJSON(w, map[string]any{
					"incident": map[string]any{"incidentID": "inc-123", "title": "Test", "status": "active"},
				})
			},
			wantID: "inc-123",
			wantPaths: []string{
				"/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.GetIncident",
				"/api/plugins/grafana-irm-app/resources/api/IncidentsService.GetIncident",
			},
		},
		{
			name: "404 on both base paths is not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantNotFound: true,
		},
		{
			name: "an empty incident is not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"incident": map[string]any{}})
			},
			wantNotFound: true,
		},
		{
			name: "propagates a server error",
			id:   "inc-err",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal error"})
			},
			wantErr: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				tt.handler(w, r)
			}))
			defer server.Close()

			result, err := newTestClient(server).Get(t.Context(), tt.id)

			switch {
			case tt.wantNotFound:
				require.ErrorIs(t, err, irm.ErrNotFound)
				assert.Contains(t, err.Error(), tt.id)
			case tt.wantErr != "":
				require.Error(t, err)
				assert.NotErrorIs(t, err, irm.ErrNotFound)
				assert.Contains(t, err.Error(), tt.wantErr)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, result.IncidentID)
			}

			if tt.wantPaths != nil {
				assert.Equal(t, tt.wantPaths, paths)
			}
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name     string
		incident irm.Incident
		handler  http.HandlerFunc
		wantBody map[string]any
		wantID   string
		wantErr  string
	}{
		{
			name:     "creates the incident",
			incident: irm.Incident{Title: "DB Outage", Status: "resolved", IsDrill: true, IncidentType: "internal"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"incident": map[string]any{"incidentID": "new-123", "title": "DB Outage", "status": "resolved"},
				})
			},
			wantBody: map[string]any{
				"title":        "DB Outage",
				"status":       "resolved",
				"isDrill":      true,
				"incidentType": "internal",
				"labels":       []any{},
			},
			wantID: "new-123",
		},
		{
			// CreateIncident ignores severity, so the request must not
			// pretend to carry one.
			name:     "defaults the status and drops the severity",
			incident: irm.Incident{Title: "DB Outage", Severity: "Critical", SeverityID: "sev-1"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"incident": map[string]any{"incidentID": "new-456", "title": "DB Outage", "severity": "Pending"},
				})
			},
			wantBody: map[string]any{
				"title":   "DB Outage",
				"status":  "active",
				"isDrill": false,
				"labels":  []any{},
			},
			wantID: "new-456",
		},
		{
			name:     "propagates a server error",
			incident: irm.Incident{Title: "DB Outage"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"message": "title is required"})
			},
			wantErr: "title is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				tt.handler(w, r)
			}))
			defer server.Close()

			inc := tt.incident
			created, err := newTestClient(server).Create(t.Context(), &inc)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/api/plugins/grafana-irm-app/resources/api/v1/IncidentsService.CreateIncident", gotPath)
			assert.Equal(t, tt.wantBody, gotBody)
			assert.Equal(t, tt.wantID, created.IncidentID)
		})
	}
}

func TestClient_AddActivity(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "adds the activity note",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{})
			},
		},
		{
			name: "propagates a server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, map[string]string{"error": "forbidden"})
			},
			wantErr: "forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var gotBody map[string]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				tt.handler(w, r)
			}))
			defer server.Close()

			err := newTestClient(server).AddActivity(t.Context(), "inc-123", "This is a note")

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/api/plugins/grafana-irm-app/resources/api/v1/ActivityService.AddActivity", gotPath)
			assert.Equal(t, map[string]string{
				"incidentID":   "inc-123",
				"activityKind": "userNote",
				"body":         "This is a note",
			}, gotBody)
		})
	}
}

// TestErrNotFoundIsComparable pins the sentinel a caller matches on: the
// error carries the incident ID and still unwraps to ErrNotFound.
func TestErrNotFoundIsComparable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := newTestClient(server).Get(t.Context(), "inc-gone")
	require.Error(t, err)
	assert.True(t, errors.Is(err, irm.ErrNotFound))
}
