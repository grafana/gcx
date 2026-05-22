package k6_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectClient_ListProjects_SendsBearerAndStackID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "v3-tok",
			})
			return
		}
		assert.Equal(t, "/cloud/v6/projects", r.URL.Path)
		assert.Equal(t, "Bearer v3-tok", r.Header.Get("Authorization"))
		assert.Equal(t, "999", r.Header.Get("X-Stack-Id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{{"id": 1, "name": "p1"}},
		})
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	require.NoError(t, client.Authenticate(t.Context(), "glsa_test", 999))
	projects, err := client.ListProjects(t.Context())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "p1", projects[0].Name)
}

func TestDirectClient_AuthenticateAndToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/v3/account/grafana-app/start" {
			assert.Equal(t, "glsa_test", r.Header.Get("X-Grafana-Service-Token"))
			assert.Equal(t, "999", r.Header.Get("X-Stack-Id"))
			assert.Equal(t, "admin", r.Header.Get("X-Grafana-User"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id":  "42",
				"v3_grafana_token": "fresh-v3-token",
			})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	err := client.Authenticate(t.Context(), "glsa_test", 999)
	require.NoError(t, err)

	tok, err := client.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fresh-v3-token", tok)
}
