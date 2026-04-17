package kg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newInternalTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := NewClient(cfg)
	require.NoError(t, err)
	return c
}

func scopesHandler(scopes map[string][]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"scopeValues": scopes})
	}
}

func TestScopeFlags_ValidateScopes(t *testing.T) {
	knownScopes := map[string][]string{
		"env":       {"ops-eu-south-0", "ops-eu-north-1", "prod-us-east-1"},
		"site":      {"site-a", "site-b"},
		"namespace": {"default", "monitoring"},
	}

	tests := []struct {
		name        string
		flags       scopeFlags
		serverScopes map[string][]string
		serverErr   bool
		wantErr     bool
		errContains string
	}{
		{
			name:         "no scope flags set — skips validation",
			flags:        scopeFlags{},
			serverScopes: knownScopes,
		},
		{
			name:         "exact match — no error",
			flags:        scopeFlags{env: "ops-eu-south-0"},
			serverScopes: knownScopes,
		},
		{
			name:         "exact match multiple flags — no error",
			flags:        scopeFlags{env: "ops-eu-south-0", namespace: "default"},
			serverScopes: knownScopes,
		},
		{
			name:         "partial match — error with candidates",
			flags:        scopeFlags{env: "ops"},
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  `did you mean one of: ops-eu-north-1, ops-eu-south-0`,
		},
		{
			name:         "no candidates — lists known values",
			flags:        scopeFlags{env: "totally-unknown"},
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  `known env values:`,
		},
		{
			name:         "known values truncated at 10 with hint",
			flags:        scopeFlags{env: "zzz-no-match"},
			serverScopes: map[string][]string{
				"env": {"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11"},
			},
			wantErr:     true,
			errContains: "and 1 more — run gcx kg scopes list",
		},
		{
			name:         "multiple invalid flags — error lists all",
			flags:        scopeFlags{env: "bad-env", site: "bad-site"},
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  "--env",
		},
		{
			name:      "API error — best-effort, no error returned",
			flags:     scopeFlags{env: "anything"},
			serverErr: true,
		},
		{
			name:         "empty known values for dimension — skips that dimension",
			flags:        scopeFlags{env: "whatever"},
			serverScopes: map[string][]string{"env": {}},
		},
		{
			name:         "case-insensitive substring match",
			flags:        scopeFlags{env: "OPS"},
			serverScopes: knownScopes,
			wantErr:      true,
			errContains:  "ops-eu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.serverErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				scopesHandler(tt.serverScopes)(w, r)
			}))
			defer server.Close()

			client := newInternalTestClient(t, server)
			err := tt.flags.validateScopes(t.Context(), client)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
