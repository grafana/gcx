package grafanaquery_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const queryPath = "/apis/datasource.grafana.app/v0alpha1/namespaces/stacks-1/query"

func TestExecute(t *testing.T) {
	tests := []struct {
		name       string
		k8sStatus  int
		legacyCode int
		wantPaths  []string
		wantErr    bool
	}{
		{
			name:      "uses the datasource.grafana.app query group",
			k8sStatus: http.StatusOK,
			wantPaths: []string{queryPath},
		},
		{
			name:       "falls back to /api/ds/query when the group is not served",
			k8sStatus:  http.StatusNotFound,
			legacyCode: http.StatusOK,
			wantPaths:  []string{queryPath, "/api/ds/query"},
		},
		{
			name:       "falls back to /api/ds/query when the group is forbidden",
			k8sStatus:  http.StatusForbidden,
			legacyCode: http.StatusOK,
			wantPaths:  []string{queryPath, "/api/ds/query"},
		},
		{
			name:       "returns the fallback error",
			k8sStatus:  http.StatusNotFound,
			legacyCode: http.StatusForbidden,
			wantPaths:  []string{queryPath, "/api/ds/query"},
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				status := tt.legacyCode
				if r.URL.Path == queryPath {
					status = tt.k8sStatus
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"results":{}}`))
			}))
			t.Cleanup(server.Close)

			client := grafanaquery.NewClientWithHTTPClient(config.NamespacedRESTConfig{
				Config:    rest.Config{Host: server.URL},
				Namespace: "stacks-1",
			}, server.Client())

			body, err := client.Execute(t.Context(), []byte(`{"queries":[]}`), "prometheus", "query")
			assert.Equal(t, tt.wantPaths, paths)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, `{"results":{}}`, string(body))
		})
	}
}
