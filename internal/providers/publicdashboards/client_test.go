package publicdashboards_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/publicdashboards"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *publicdashboards.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := publicdashboards.NewClient(cfg)
	require.NoError(t, err)
	return client
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

func TestClient_List(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantLen  int
		wantErr  bool
		firstUID string
	}{
		{
			name: "success with items unwraps publicDashboards",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/dashboards/public-dashboards", r.URL.Path)
				writeJSON(w, map[string]any{
					"publicDashboards": []publicdashboards.PublicDashboard{
						{UID: "pd-1", DashboardUID: "d-1", IsEnabled: true},
						{UID: "pd-2", DashboardUID: "d-2"},
					},
				})
			},
			wantLen:  2,
			firstUID: "pd-1",
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"publicDashboards": []publicdashboards.PublicDashboard{}})
			},
			wantLen: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"boom"}`)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			list, err := client.List(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, list, tt.wantLen)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.firstUID, list[0].UID)
			}
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name         string
		dashboardUID string
		handler      http.HandlerFunc
		wantErr      bool
	}{
		{
			name:         "success escapes uid",
			dashboardUID: "abc 123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				// r.URL.Path is the decoded form; the raw wire path comes through RequestURI.
				assert.Equal(t, "/api/dashboards/uid/abc%20123/public-dashboards", r.RequestURI)
				writeJSON(w, publicdashboards.PublicDashboard{UID: "pd-1", DashboardUID: "abc 123", IsEnabled: true})
			},
		},
		{
			name:         "404 error",
			dashboardUID: "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"message":"not found"}`)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			pd, err := client.Get(t.Context(), tt.dashboardUID)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, pd)
			assert.Equal(t, "pd-1", pd.UID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/dashboards/uid/dash-1/public-dashboards", r.URL.Path)

		var body publicdashboards.PublicDashboard
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		assert.True(t, body.IsEnabled)
		assert.Equal(t, "public", body.Share)

		writeJSON(w, publicdashboards.PublicDashboard{
			UID:          "pd-new",
			DashboardUID: "dash-1",
			AccessToken:  "token-xyz",
			IsEnabled:    true,
			Share:        "public",
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	created, err := client.Create(t.Context(), "dash-1", &publicdashboards.PublicDashboard{
		IsEnabled: true,
		Share:     "public",
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "pd-new", created.UID)
	assert.Equal(t, "token-xyz", created.AccessToken)
}

func TestClient_Update(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/dashboards/uid/dash-1/public-dashboards/pd-1", r.URL.Path)

		var body publicdashboards.PublicDashboard
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		assert.True(t, body.AnnotationsEnabled)

		writeJSON(w, publicdashboards.PublicDashboard{
			UID:                "pd-1",
			DashboardUID:       "dash-1",
			AnnotationsEnabled: true,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	updated, err := client.Update(t.Context(), "dash-1", "pd-1", &publicdashboards.PublicDashboard{
		AnnotationsEnabled: true,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "pd-1", updated.UID)
	assert.True(t, updated.AnnotationsEnabled)
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/dashboards/uid/dash-1/public-dashboards/pd-1", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"oops"}`)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Delete(t.Context(), "dash-1", "pd-1")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
