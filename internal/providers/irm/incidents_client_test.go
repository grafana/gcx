package irm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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

// listRequest mirrors the QueryIncidents request body for assertions.
type listRequest struct {
	Query  map[string]any  `json:"query"`
	Cursor *map[string]any `json:"cursor"`
}

// incidentPage builds a one-page response with n incidents named after page.
func incidentPage(page string, n int, hasMore bool, nextValue string) map[string]any {
	incs := make([]map[string]any, n)
	for i := range incs {
		incs[i] = map[string]any{"incidentID": fmt.Sprintf("inc-%s-%d", page, i), "title": "Outage " + page, "status": "active"}
	}
	return map[string]any{
		"incidents": incs,
		"cursor":    map[string]any{"hasMore": hasMore, "nextValue": nextValue},
		"query":     map[string]any{},
	}
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name      string
		query     irm.IncidentQuery
		handler   func(t *testing.T, calls *[]listRequest) http.HandlerFunc
		wantLen   int
		wantCalls int
		wantErr   string
	}{
		{
			name:  "returns incidents",
			query: irm.IncidentQuery{Limit: 50},
			handler: func(t *testing.T, _ *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Contains(t, r.URL.Path, "IncidentsService.QueryIncidents")
					writeJSON(w, incidentPage("a", 2, false, ""))
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
						writeJSON(w, incidentPage("p1", 1, true, "cursor-1"))
					default:
						req := (*calls)[1]
						if assert.NotNil(t, req.Cursor, "second request must carry the cursor next to the query") {
							// The previously returned cursor is echoed back verbatim.
							assert.Equal(t, "cursor-1", (*req.Cursor)["nextValue"])
							assert.Equal(t, true, (*req.Cursor)["hasMore"])
						}
						assert.NotContains(t, req.Query, "contextPayload")
						writeJSON(w, incidentPage("p2", 1, false, ""))
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
						writeJSON(w, incidentPage("p1", 100, true, "cursor-1"))
					case 2:
						// 150 remain wanted; the page request must ask for 100.
						assert.InDelta(t, 100, req.Query["limit"], 0)
						writeJSON(w, incidentPage("p2", 100, true, "cursor-2"))
					default:
						// 50 remain wanted; the page request must shrink.
						assert.InDelta(t, 50, req.Query["limit"], 0)
						writeJSON(w, incidentPage("p3", 50, false, ""))
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
					writeJSON(w, incidentPage("p1", 1, true, "cursor-1"))
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
					writeJSON(w, incidentPage("p1", 5, false, ""))
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
					writeJSON(w, incidentPage("p1", 1, true, ""))
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
					writeJSON(w, incidentPage("p1", 100, true, "cursor-1"))
				}
			},
			wantLen:   100,
			wantCalls: 1,
		},
		{
			name:  "forwards filters in the query",
			query: irm.IncidentQuery{Limit: 10, IncidentLabels: []string{"security"}},
			handler: func(t *testing.T, calls *[]listRequest) http.HandlerFunc {
				t.Helper()
				return func(w http.ResponseWriter, _ *http.Request) {
					q := (*calls)[0].Query
					assert.Equal(t, []any{"security"}, q["incidentLabels"])
					assert.Equal(t, "DESC", q["orderDirection"])
					writeJSON(w, incidentPage("p1", 1, false, ""))
				}
			},
			wantLen:   1,
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
		t.Run(tt.name, func(t *testing.T) {
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
			assert.Len(t, result, tt.wantLen)
			assert.Len(t, calls, tt.wantCalls)
		})
	}
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
