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
