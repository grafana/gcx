package investigations_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// newTestClient builds an investigations client wired to an httptest server.
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
