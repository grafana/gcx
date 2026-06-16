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

// fakeFallback is a direct-SM-API credential resolver for fallback tests. It
// records how many times it was invoked so tests can assert the fallback was
// (or was not) taken.
type fakeFallback struct {
	baseURL string
	token   string
	err     error
	calls   int
}

func (f *fakeFallback) LoadSMConfig(context.Context) (string, string, string, error) {
	f.calls++
	return f.baseURL, f.token, "", f.err
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

// TestClient_FallsBackToDirectOn403 verifies the core dual-mode contract: a 403
// from the proxy drops the request to the direct SM API, where the resolved
// token is used and the /api/v1 path is hit.
func TestClient_FallsBackToDirectOn403(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, proxyPath("check/list"), r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"plugin proxy route access denied"}`))
	}))
	defer proxySrv.Close()

	var directHit bool
	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHit = true
		assert.Equal(t, "/api/v1/check/list", r.URL.Path)
		assert.Equal(t, "Bearer direct-token", r.Header.Get("Authorization"))
		writeJSON(w, []checks.Check{{ID: 7, Job: "fallback-job"}})
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: proxySrv.URL}}
	client, err := checks.NewClient(cfg, testDSUID, fallback)
	require.NoError(t, err)

	got, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "fallback-job", got[0].Job)
	assert.True(t, directHit, "direct SM API must be hit after a proxy 403")
	assert.Equal(t, 1, fallback.calls, "fallback credentials must be resolved exactly once")
}

// TestClient_NoFallbackOn404 verifies that a proxy 404 is treated as a real SM
// response (check-not-found), NOT a fallback trigger.
func TestClient_NoFallbackOn404(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer proxySrv.Close()

	fallback := &fakeFallback{baseURL: "http://unused", token: "x"}
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: proxySrv.URL}}
	client, err := checks.NewClient(cfg, testDSUID, fallback)
	require.NoError(t, err)

	_, err = client.Get(context.Background(), 999)
	require.ErrorIs(t, err, checks.ErrNotFound)
	assert.Equal(t, 0, fallback.calls, "404 must not trigger the direct fallback")
}

// TestClient_DirectOnlyWhenNoUID verifies that an empty datasource UID skips the
// proxy entirely and goes straight to the direct SM API.
func TestClient_DirectOnlyWhenNoUID(t *testing.T) {
	var directHit bool
	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHit = true
		assert.Equal(t, "/api/v1/check/list", r.URL.Path)
		writeJSON(w, []checks.Check{{ID: 1}})
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	client, err := checks.NewClient(config.NamespacedRESTConfig{}, "", fallback)
	require.NoError(t, err)

	got, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, directHit)
	assert.Equal(t, 1, fallback.calls)
}

// TestClient_Proxy403NoFallbackConfigured verifies that a proxy 403 with no
// fallback loader surfaces an error rather than silently succeeding.
func TestClient_Proxy403NoFallbackConfigured(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxySrv.Close()

	_, err := proxyClient(t, proxySrv).List(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no direct SM API fallback")
}

// TestClient_BothPathsFail verifies that when the proxy 403s and the direct API
// also errors, the direct error surfaces.
func TestClient_BothPathsFail(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxySrv.Close()

	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "direct boom"})
	}))
	defer directSrv.Close()

	fallback := &fakeFallback{baseURL: directSrv.URL, token: "direct-token"}
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: proxySrv.URL}}
	client, err := checks.NewClient(cfg, testDSUID, fallback)
	require.NoError(t, err)

	_, err = client.List(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "direct boom")
}
