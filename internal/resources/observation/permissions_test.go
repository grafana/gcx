package observation_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionReader(t *testing.T) {
	acl := map[string]string{
		"a": `[{"userLogin":"sa-1","permissionName":"Admin","updated":"t1"},{"role":"Editor","permissionName":"Edit"},{"role":"Viewer","permissionName":"View"}]`,
		"b": `[{"role":"Viewer","permissionName":"View"},{"role":"Editor","permissionName":"Edit"},{"userLogin":"sa-1","permissionName":"Admin","updated":"t2"}]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, body := range acl {
			if r.URL.Path == fmt.Sprintf("/api/dashboards/uid/%s/permissions", name) {
				_, _ = fmt.Fprint(w, body)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	cov, objs, err := observation.NewPermissionReader(srv.Client(), srv.URL)(context.Background(), observation.TypeDashboards, []string{"a", "b", "gone"})
	require.NoError(t, err)
	assert.Equal(t, observation.CoverageComplete, cov.Status)
	assert.Equal(t, observation.TypeDashboardPermissions, cov.Type)
	// A resource deleted before its read is omitted, not a failure.
	assert.Len(t, objs, 2)
	// Order and timestamps do not matter: both lists normalize identically.
	assert.Equal(t, objs["a"]["spec"], objs["b"]["spec"])
	spec, ok := objs["a"]["spec"].(map[string]any)
	require.True(t, ok)
	entries, ok := spec["entries"].([]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"principal": "role:Editor", "permission": "Edit", "inherited": false}, entries[0])
}
