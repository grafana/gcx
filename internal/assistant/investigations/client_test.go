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
		state     string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name:  "success",
			state: "",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/investigations/summary")
				writeJSON(w, []investigations.InvestigationSummary{
					{ID: "inv-1", Title: "Test", Status: "running"},
					{ID: "inv-2", Title: "Test 2", Status: "completed"},
				})
			},
			wantCount: 2,
		},
		{
			name:  "filter by state",
			state: "completed",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "completed", r.URL.Query().Get("state"))
				writeJSON(w, []investigations.InvestigationSummary{
					{ID: "inv-2", Title: "Test 2", Status: "completed"},
				})
			},
			wantCount: 1,
		},
		{
			name:  "empty list",
			state: "",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []investigations.InvestigationSummary{})
			},
			wantCount: 0,
		},
		{
			name:  "server error",
			state: "",
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
			summaries, err := client.List(t.Context(), tt.state)
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
				writeJSON(w, investigations.Investigation{"id": "inv-1", "title": "Test", "status": "running"})
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
				writeJSON(w, investigations.CreateResponse{ID: "inv-new", Status: "running"})
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
			assert.Equal(t, "running", resp.Status)
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
				writeJSON(w, investigations.CancelResponse{ID: "inv-1", Status: "cancelled"})
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
			assert.Equal(t, "inv-1", resp.ID)
			assert.Equal(t, "cancelled", resp.Status)
		})
	}
}

func TestTodos(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/todos")
		writeJSON(w, []investigations.Todo{
			{ID: "t-1", Title: "Check alerts", Status: "completed"},
			{ID: "t-2", Title: "Analyze logs", Status: "in_progress", Assignee: "agent"},
		})
	}))

	todos, err := client.Todos(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Len(t, todos, 2)
	assert.Equal(t, "Check alerts", todos[0].Title)
}

func TestTimeline(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/timeline-snapshot")
		writeJSON(w, []investigations.TimelineEntry{
			{Timestamp: now, Type: "task_started", Summary: "Started check alerts", Actor: "agent"},
		})
	}))

	entries, err := client.Timeline(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "task_started", entries[0].Type)
}

func TestReport(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/report-summary")
		writeJSON(w, investigations.ReportSummary{"summary": "All clear", "status": "completed"})
	}))

	report, err := client.Report(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Equal(t, "All clear", (*report)["summary"])
}

func TestDocument(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/investigations/inv-1/documents/doc-1")
		writeJSON(w, investigations.Document{ID: "doc-1", Title: "Alert Analysis", Content: "Details here", Type: "markdown"})
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
		writeJSON(w, []investigations.Approval{
			{ID: "a-1", Status: "pending", Approver: "user@grafana.com", CreatedAt: now},
		})
	}))

	approvals, err := client.Approvals(t.Context(), "inv-1")
	require.NoError(t, err)
	assert.Len(t, approvals, 1)
	assert.Equal(t, "pending", approvals[0].Status)
}

func TestList_NullResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("null"))
	}))

	summaries, err := client.List(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, summaries)
}
