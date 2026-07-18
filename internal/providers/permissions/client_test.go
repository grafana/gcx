package permissions_test

import (
	"context"
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

func newClient(t *testing.T, url string) *permissions.Client {
	t.Helper()
	c, err := permissions.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: url}})
	require.NoError(t, err)
	return c
}

func TestClient_Describe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/access-control/dashboards/description", r.URL.Path)
		_, _ = w.Write([]byte(`{"assignments":{"users":true,"teams":true,"serviceAccounts":true,"builtInRoles":true},"permissions":["View","Edit","Admin"]}`))
	}))
	defer srv.Close()

	desc, err := newClient(t, srv.URL).Describe(context.Background(), "dashboards")
	require.NoError(t, err)
	assert.Equal(t, []string{"View", "Edit", "Admin"}, desc.Permissions)
	assert.True(t, desc.Assignments.ServiceAccounts)
}

func TestClient_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/access-control/folders/my-uid", r.URL.Path)
		_, _ = w.Write([]byte(`[{"id":1,"builtInRole":"Editor","permission":"Edit","isManaged":true},{"id":2,"userLogin":"alice","permission":"Admin"}]`))
	}))
	defer srv.Close()

	perms, err := newClient(t, srv.URL).Get(context.Background(), "folders", "my-uid")
	require.NoError(t, err)
	require.Len(t, perms, 2)
	assert.Equal(t, "Editor", perms[0].BuiltInRole)
	assert.Equal(t, "alice", perms[1].UserLogin)
}

func TestClient_Set(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/access-control/dashboards/my-uid", r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"message":"Permissions updated"}`))
	}))
	defer srv.Close()

	err := newClient(t, srv.URL).Set(context.Background(), "dashboards", "my-uid", []permissions.SetResourcePermissionCommand{
		{TeamID: 3, Permission: "Edit"},
		{BuiltInRole: "Viewer", Permission: "View"},
	})
	require.NoError(t, err)
	perms, ok := gotBody["permissions"].([]any)
	require.True(t, ok)
	assert.Len(t, perms, 2)
}

func TestClient_SetGrantsPerPrincipal(t *testing.T) {
	tests := []struct {
		name     string
		call     func(c *permissions.Client) error
		wantPath string
	}{
		{
			name: "user",
			call: func(c *permissions.Client) error {
				return c.SetUserPermission(context.Background(), "dashboards", "d1", "42", "Edit")
			},
			wantPath: "/api/access-control/dashboards/d1/users/42",
		},
		{
			name: "team",
			call: func(c *permissions.Client) error {
				return c.SetTeamPermission(context.Background(), "folders", "f1", "3", "Admin")
			},
			wantPath: "/api/access-control/folders/f1/teams/3",
		},
		{
			name: "role",
			call: func(c *permissions.Client) error {
				return c.SetBuiltInRolePermission(context.Background(), "datasources", "ds1", "Viewer", "View")
			},
			wantPath: "/api/access-control/datasources/ds1/builtInRoles/Viewer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotLevel string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				var body map[string]string
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &body)
				gotLevel = body["permission"]
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			require.NoError(t, tt.call(newClient(t, srv.URL)))
			assert.Equal(t, tt.wantPath, gotPath)
			assert.NotEmpty(t, gotLevel)
		})
	}
}

func TestClient_SetForbiddenHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"access denied"}`))
	}))
	defer srv.Close()

	err := newClient(t, srv.URL).SetTeamPermission(context.Background(), "dashboards", "d1", "3", "Edit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "requires RBAC")
}
