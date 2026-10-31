package config_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestConfiguredOrgScopesGrafanaRequests(t *testing.T) {
	for _, test := range []struct {
		name     string
		org      int64
		override string
		want     string
	}{
		{"configured org", 99999, "", "99999"},
		{"unset org", 0, "", ""},
		{"explicit header", 99999, "12345", "12345"},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, test.want, r.Header.Get("X-Grafana-Org-Id"))
				user, password, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "user", user)
				assert.Equal(t, "password", password)
				w.WriteHeader(http.StatusForbidden)
			}))
			defer srv.Close()
			cfg, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{Grafana: &config.GrafanaConfig{Server: srv.URL, StackID: 12345, OrgID: test.org, User: "user", Password: "password"}})
			require.NoError(t, err)
			client, err := rest.HTTPClientFor(&cfg.Config)
			require.NoError(t, err)
			for _, path := range []string{"/api/datasources", "/api/ds/query", "/apis/query.grafana.app/v0alpha1/namespaces/stacks-12345/query", "/api/plugins/example/resources/settings"} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
				require.NoError(t, err)
				if test.override != "" {
					req.Header.Set("X-Grafana-Org-Id", test.override)
				}
				resp, err := client.Do(req)
				require.NoError(t, err)
				require.Equal(t, http.StatusForbidden, resp.StatusCode)
				_, _ = io.Copy(io.Discard, resp.Body)
				require.NoError(t, resp.Body.Close())
			}
			external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Header.Get("X-Grafana-Org-Id"))
				w.WriteHeader(http.StatusOK)
			}))
			defer external.Close()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, external.URL, nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestConfiguredOrgStaysWithinGrafanaSubpath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := ""
		if r.URL.Path == "/grafana/api/datasources" {
			want = "99999"
		}
		assert.Equal(t, want, r.Header.Get("X-Grafana-Org-Id"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{Grafana: &config.GrafanaConfig{Server: server.URL + "/grafana", StackID: 12345, OrgID: 99999, APIToken: "token"}})
	require.NoError(t, err)
	client, err := rest.HTTPClientFor(&cfg.Config)
	require.NoError(t, err)
	for _, path := range []string{"/grafana/api/datasources", "/grafana-other/api/datasources", "/api/product"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
}
