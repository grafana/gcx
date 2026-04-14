package investigations_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *investigations.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return investigations.NewClient(base)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name      string
		opts      investigations.ListOptions
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/investigations/summary")
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"investigations": []investigations.InvestigationSummary{
							{ID: "inv-1", Title: "Test", State: "running"},
							{ID: "inv-2", Title: "Test 2", State: "completed"},
						},
					},
				})
			},
			wantCount: 2,
		},
		{
			name: "filter by state",
			opts: investigations.ListOptions{State: "completed"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "completed", r.URL.Query().Get("state"))
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"investigations": []investigations.InvestigationSummary{
							{ID: "inv-2", Title: "Test 2", State: "completed"},
						},
					},
				})
			},
			wantCount: 1,
		},
		{
			name: "pagination params",
			opts: investigations.ListOptions{Limit: 10, Offset: 20},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "10", r.URL.Query().Get("limit"))
				assert.Equal(t, "20", r.URL.Query().Get("offset"))
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"investigations": []investigations.InvestigationSummary{
							{ID: "inv-3", Title: "Test 3", State: "running"},
						},
					},
				})
			},
			wantCount: 1,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"investigations": []investigations.InvestigationSummary{},
					},
				})
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			summaries, err := client.List(t.Context(), tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, summaries, tt.wantCount)
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/investigations/inv-1")
				writeJSON(w, map[string]any{
					"data": investigations.Investigation{"id": "inv-1", "title": "Test", "status": "running"},
				})
			},
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			inv, err := client.Get(t.Context(), "inv-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "inv-1", (*inv)["id"])
		})
	}
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/investigations")

				body, _ := io.ReadAll(r.Body)
				var req investigations.CreateRequest
				assert.NoError(t, json.Unmarshal(body, &req))
				assert.Equal(t, "Test Investigation", req.Title)
				assert.Equal(t, "Looking into alerts", req.Description)

				w.WriteHeader(http.StatusCreated)
				writeJSON(w, map[string]any{
					"data": investigations.CreateResponse{ID: "inv-new", State: "running"},
				})
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad request"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			resp, err := client.Create(t.Context(), investigations.CreateRequest{
				Title:       "Test Investigation",
				Description: "Looking into alerts",
			})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "inv-new", resp.ID)
			assert.Equal(t, "running", resp.State)
		})
	}
}

func TestCancel(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/investigations/inv-1/cancel")
				writeJSON(w, map[string]any{
					"data": investigations.CancelResponse{Message: "Investigation cancelled."},
				})
			},
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			resp, err := client.Cancel(t.Context(), "inv-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "Investigation cancelled.", resp.Message)
		})
	}
}

func TestTodos(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/timeline-snapshot")
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"agents": []investigations.TimelineAgent{
					{AgentID: "a-1", AgentName: "Check alerts", Status: "completed", MessageCount: 3},
					{AgentID: "a-2", AgentName: "Analyze logs", Status: "in_progress", MessageCount: 1},
				},
			},
		})
	}))

	todos, err := client.Todos(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Len(t, todos, 2)
	assert.Equal(t, "Check alerts", todos[0].Title)
}

func TestTimeline(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/timeline-snapshot")
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"agents": []investigations.TimelineAgent{
					{AgentID: "a-1", AgentName: "investigation_lead", Status: "completed", MessageCount: 5, StartTime: 1700000000000, LastActivity: 1700000300000},
				},
			},
		})
	}))

	agents, err := client.Timeline(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, "investigation_lead", agents[0].AgentName)
}

func TestReport(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/report-summary")
		writeJSON(w, map[string]any{
			"data": investigations.ReportSummary{"summary": "All clear", "phase": "completed"},
		})
	}))

	report, err := client.Report(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Equal(t, "All clear", (*report)["summary"])
}

func TestDocument(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/documents/doc-1")
		writeJSON(w, map[string]any{
			"data": investigations.Document{ID: "doc-1", Title: "Alert Analysis", Content: "Details here", Type: "markdown"},
		})
	}))

	doc, err := client.Document(t.Context(), "inv-1", "doc-1")
	require.NoError(t, err)
	assert.Equal(t, "doc-1", doc.ID)
	assert.Equal(t, "Alert Analysis", doc.Title)
}

func TestApprovals(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/approvals")
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"approvals": []investigations.Approval{
					{ID: "a-1", Status: "pending", Approver: "user@grafana.com", CreatedAt: now},
				},
			},
		})
	}))

	approvals, err := client.Approvals(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Len(t, approvals, 1)
	assert.Equal(t, "pending", approvals[0].Status)
}

func TestTimeline_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))

	_, err := client.Timeline(t.Context(), "inv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestTimeline_NullAgents(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"agents": nil,
			},
		})
	}))

	agents, err := client.Timeline(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestReport_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))

	_, err := client.Report(t.Context(), "inv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestDocument_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))

	_, err := client.Document(t.Context(), "inv-1", "doc-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestApprovals_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))

	_, err := client.Approvals(t.Context(), "inv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestApprovals_NullList(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"approvals": nil,
			},
		})
	}))

	approvals, err := client.Approvals(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Empty(t, approvals)
}

func TestTodos_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))

	_, err := client.Todos(t.Context(), "inv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestTodos_EmptyAgents(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"agents": []investigations.TimelineAgent{},
			},
		})
	}))

	todos, err := client.Todos(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Empty(t, todos)
}

func TestGet_InvalidJSON(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))

	_, err := client.Get(t.Context(), "inv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestList_InvalidJSON(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))

	_, err := client.List(t.Context(), investigations.ListOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestList_NullResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"investigations": nil,
			},
		})
	}))

	summaries, err := client.List(t.Context(), investigations.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, summaries)
}
