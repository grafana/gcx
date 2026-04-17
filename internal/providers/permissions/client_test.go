package permissions_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *permissions.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := permissions.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

// decodeSetBody reads a POST permissions request body and returns the contained items.
func decodeSetBody(t *testing.T, r *http.Request) []permissions.Item {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	var received struct {
		Items []permissions.Item `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &received))
	return received.Items
}

func TestClient_GetFolder(t *testing.T) {
	tests := []struct {
		name      string
		uid       string
		handler   http.HandlerFunc
		wantItems int
		wantErr   bool
	}{
		{
			name: "success",
			uid:  "folder-abc",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/folders/folder-abc/permissions", r.URL.Path)
				writeJSON(w, []permissions.Item{
					{Role: "Viewer", Permission: 1},
					{Role: "Editor", Permission: 2},
				})
			},
			wantItems: 2,
		},
		{
			name: "uid needs escaping",
			uid:  "folder with spaces",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/folders/folder%20with%20spaces/permissions", r.URL.EscapedPath())
				writeJSON(w, []permissions.Item{})
			},
			wantItems: 0,
		},
		{
			name: "server error",
			uid:  "x",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, permissions.ErrorResponse{Error: "boom"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			items, err := client.GetFolder(t.Context(), tt.uid)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, items, tt.wantItems)
		})
	}
}

//nolint:dupl // near-duplicate of TestClient_SetDashboard; kept readable as two focused tests.
func TestClient_SetFolder(t *testing.T) {
	tests := []struct {
		name    string
		uid     string
		items   []permissions.Item
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:  "success",
			uid:   "folder-abc",
			items: []permissions.Item{{Role: "Viewer", Permission: 1}},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/folders/folder-abc/permissions", r.URL.Path)
				items := decodeSetBody(t, r)
				assert.Len(t, items, 1)
				assert.Equal(t, "Viewer", items[0].Role)
				assert.Equal(t, 1, items[0].Permission)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"message":"ok"}`))
			},
		},
		{
			name:  "server error",
			uid:   "folder-abc",
			items: []permissions.Item{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, permissions.ErrorResponse{Error: "bad request"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.SetFolder(t.Context(), tt.uid, tt.items)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_GetDashboard(t *testing.T) {
	tests := []struct {
		name      string
		uid       string
		handler   http.HandlerFunc
		wantItems int
		wantErr   bool
	}{
		{
			name: "success",
			uid:  "dash-xyz",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/dashboards/uid/dash-xyz/permissions", r.URL.Path)
				writeJSON(w, []permissions.Item{
					{UserLogin: "admin", Permission: 4},
				})
			},
			wantItems: 1,
		},
		{
			name: "not found",
			uid:  "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, permissions.ErrorResponse{Error: "not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			items, err := client.GetDashboard(t.Context(), tt.uid)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, items, tt.wantItems)
		})
	}
}

//nolint:dupl // near-duplicate of TestClient_SetFolder; kept readable as two focused tests.
func TestClient_SetDashboard(t *testing.T) {
	tests := []struct {
		name    string
		uid     string
		items   []permissions.Item
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:  "success",
			uid:   "dash-xyz",
			items: []permissions.Item{{TeamID: 5, Permission: 2}},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/dashboards/uid/dash-xyz/permissions", r.URL.Path)
				items := decodeSetBody(t, r)
				assert.Len(t, items, 1)
				assert.Equal(t, 5, items[0].TeamID)
				assert.Equal(t, 2, items[0].Permission)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"message":"ok"}`))
			},
		},
		{
			name:  "server error",
			uid:   "dash-xyz",
			items: []permissions.Item{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, permissions.ErrorResponse{Error: "forbidden"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.SetDashboard(t.Context(), tt.uid, tt.items)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
