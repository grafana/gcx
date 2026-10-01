package host_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardTransport(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
	}))
	defer srv.Close()
	client := &http.Client{Transport: host.GuardTransport(nil)}

	tests := []struct {
		access  host.Access
		method  string
		path    string
		allowed bool
	}{
		{host.AccessRead, http.MethodGet, "/api/dashboards/uid/x", true},
		{host.AccessRead, http.MethodHead, "/api/health", true},
		{host.AccessRead, http.MethodPost, "/api/ds/query", true},
		{host.AccessRead, http.MethodPost, "/apis/query.grafana.app/v0alpha1/namespaces/stacks-1/query", true},
		{host.AccessRead, http.MethodPost, "/api/dashboards/db", false},
		{host.AccessRead, http.MethodPut, "/api/folders/x", false},
		{host.AccessRead, http.MethodDelete, "/api/folders/x", false},
		{host.AccessWrite, http.MethodPost, "/api/dashboards/db", true},
		{host.AccessWrite, http.MethodPatch, "/api/folders/x", true},
		{host.AccessWrite, http.MethodDelete, "/api/folders/x", false},
		{host.AccessDelete, http.MethodDelete, "/api/folders/x", true},
		{host.AccessDelete, "PROPFIND", "/x", true},
		{host.AccessWrite, "PROPFIND", "/x", false},
	}
	for _, tt := range tests {
		t.Run(tt.access.String()+" "+tt.method+" "+tt.path, func(t *testing.T) {
			methods = nil
			ctx := host.WithSandbox(t.Context(), &host.Sandbox{Access: tt.access})
			req, err := http.NewRequestWithContext(ctx, tt.method, srv.URL+tt.path, strings.NewReader("{}"))
			require.NoError(t, err)
			resp, err := client.Do(req)
			if tt.allowed {
				require.NoError(t, err)
				resp.Body.Close()
				assert.Equal(t, []string{tt.method + " " + tt.path}, methods)
				return
			}
			require.ErrorIs(t, err, host.ErrAccessDenied)
			assert.Empty(t, methods, "a denied request must never be sent")
		})
	}

	t.Run("outside a sandbox everything passes", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, srv.URL+"/x", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	})
}
