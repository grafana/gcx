package kg_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/query/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetStatus(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "returns status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "v1/stack/status")
				writeJSON(w, kg.Status{Status: "complete", Enabled: true})
			},
		},
		{
			name: "handles error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			status, err := client.GetStatus(t.Context())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "complete", status.Status)
			assert.True(t, status.Enabled)
		})
	}
}

// TestClient_Active is the load-bearing truth table for the KG activation
// contract: only a definitive 404, an explicit enabled:false, or a
// status other than StatusComplete (still onboarding) yields (false, nil);
// every other failure mode must be (false, non-nil error) so callers never
// mistake an inconclusive check for "not activated".
func TestClient_Active(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		unreach    bool
		wantActive bool
		wantErr    bool
	}{
		{
			name: "200 enabled true and status complete",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.Status{Enabled: true, Status: kg.StatusComplete})
			},
			wantActive: true,
		},
		{
			name: "200 enabled false",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.Status{Enabled: false, Status: kg.StatusComplete})
			},
			wantActive: false,
		},
		{
			name: "200 enabled true but still onboarding",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.Status{Enabled: true, Status: "onboarding"})
			},
			wantActive: false,
		},
		{
			name: "404 not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantActive: false,
		},
		{
			name: "403 forbidden is inconclusive",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			wantErr: true,
		},
		{
			name: "500 server error is inconclusive",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
		{
			name: "malformed body is inconclusive",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("{not json"))
			},
			wantErr: true,
		},
		{
			name:    "transport failure is inconclusive",
			unreach: true,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *kg.Client
			if tt.unreach {
				server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				closedURL := server.URL
				server.Close()
				var err error
				client, err = kg.NewClient(newRESTConfig(closedURL))
				require.NoError(t, err)
			} else {
				server := httptest.NewServer(tt.handler)
				defer server.Close()
				client = newTestClient(t, server)
			}

			active, err := client.Active(t.Context())
			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, active)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantActive, active)
		})
	}
}
