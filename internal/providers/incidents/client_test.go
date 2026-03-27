package incidents_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/incidents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *incidents.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := incidents.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns incidents",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "IncidentsService.QueryIncidents")
				writeJSON(w, map[string]any{
					"incidents": []map[string]any{
						{"incidentID": "inc-1", "title": "Outage 1", "status": "active"},
						{"incidentID": "inc-2", "title": "Outage 2", "status": "resolved"},
					},
					"cursor": map[string]any{"hasMore": false},
					"query":  map[string]any{},
				})
			},
			wantLen: 2,
		},
		{
			name: "handles pagination",
			handler: func() http.HandlerFunc {
				call := 0
				return func(w http.ResponseWriter, _ *http.Request) {
					call++
					if call == 1 {
						writeJSON(w, map[string]any{
							"incidents": []map[string]any{
								{"incidentID": "inc-1", "title": "Page 1", "status": "active"},
							},
							"cursor": map[string]any{"hasMore": true, "nextValue": "cursor-1"},
							"query":  map[string]any{},
						})
					} else {
						writeJSON(w, map[string]any{
							"incidents": []map[string]any{
								{"incidentID": "inc-2", "title": "Page 2", "status": "active"},
							},
							"cursor": map[string]any{"hasMore": false},
							"query":  map[string]any{},
						})
					}
				}
			}(),
			wantLen: 2,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.List(t.Context(), incidents.IncidentQuery{})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)
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
	inc, err := c.Create(t.Context(), &incidents.Incident{
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
