package checks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const testDSUID = "sm-ds-uid"

// proxyPath is the datasource-proxy path the dual-mode client builds for a given
// logical SM API path (e.g. "check/list").
func proxyPath(smPath string) string {
	return "/api/datasources/proxy/uid/" + testDSUID + "/sm/" + smPath
}

// proxyClient returns a proxy-only client (no direct fallback) pointed at srv.
func proxyClient(t *testing.T, srv *httptest.Server) *checks.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}}
	client, err := checks.NewClient(cfg, testDSUID, nil)
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

func TestClient_List(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantChecks int
		wantErr    bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, proxyPath("check/list"), r.URL.Path)
				writeJSON(w, []checks.Check{
					{ID: 1, Job: "job-1", Target: "https://example.com"},
					{ID: 2, Job: "job-2", Target: "https://example.org"},
				})
			},
			wantChecks: 2,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []checks.Check{})
			},
			wantChecks: 0,
		},
		{
			name: "null response returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("null"))
			},
			wantChecks: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).List(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tc.wantChecks)
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		handler http.HandlerFunc
		wantJob string
		wantErr bool
		errIs   error
	}{
		{
			name: "success",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, proxyPath("check/42"), r.URL.Path)
				writeJSON(w, checks.Check{ID: 42, Job: "my-job"})
			},
			wantJob: "my-job",
		},
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
			errIs:   checks.ErrNotFound,
		},
		{
			name: "server error",
			id:   1,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).Get(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errIs != nil {
					require.ErrorIs(t, err, tc.errIs)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantJob, got.Job)
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name    string
		check   checks.Check
		handler http.HandlerFunc
		wantID  int64
		wantErr bool
	}{
		{
			name:  "success",
			check: checks.Check{Job: "new-job", Target: "https://example.com"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, proxyPath("check/add"), r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body checks.Check
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, checks.Check{ID: 100, Job: body.Job})
			},
			wantID: 100,
		},
		{
			name:  "server error",
			check: checks.Check{Job: "bad-job"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid check"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).Create(context.Background(), tc.check)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name    string
		check   checks.Check
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:  "success",
			check: checks.Check{ID: 42, TenantID: 1, Job: "updated-job"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, proxyPath("check/update"), r.URL.Path)
				var body checks.Check
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				assert.Equal(t, int64(42), body.ID)
				writeJSON(w, body)
			},
		},
		{
			name:  "server error",
			check: checks.Check{ID: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			_, err := proxyClient(t, srv).Update(context.Background(), tc.check)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 200",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, proxyPath("check/delete/42"), r.URL.Path)
				writeJSON(w, map[string]string{"msg": "ok"})
			},
		},
		{
			name: "success 204",
			id:   43,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			err := proxyClient(t, srv).Delete(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_GetTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, proxyPath("tenant"), r.URL.Path)
		writeJSON(w, checks.Tenant{ID: 214})
	}))
	defer srv.Close()

	tenant, err := proxyClient(t, srv).GetTenant(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(214), tenant.ID)
}

func TestClient_ListProbes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, proxyPath("probe/list"), r.URL.Path)
		writeJSON(w, []map[string]any{
			{"id": 1, "name": "Oregon"},
			{"id": 2, "name": "Paris"},
		})
	}))
	defer srv.Close()

	probes, err := proxyClient(t, srv).ListProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, probes, 2)
	assert.Equal(t, int64(1), probes[0].ID)
	assert.Equal(t, "Oregon", probes[0].Name)
}
