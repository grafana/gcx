package probes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		wantProbes int
		wantErr    bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/v1/probe/list", r.URL.Path)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				writeJSON(w, []probes.Probe{
					{ID: 1, Name: "Oregon", Region: "US"},
					{ID: 2, Name: "Paris", Region: "EU"},
					{ID: 3, Name: "Spain", Region: "EU"},
				})
			},
			wantProbes: 3,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []probes.Probe{})
			},
			wantProbes: 0,
		},
		{
			name: "null response returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("null"))
			},
			wantProbes: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal server error"})
			},
			wantErr: true,
		},
		{
			name: "auth error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]string{"msg": "unauthorized"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := probes.NewClient(srv.URL, "test-token")
			got, err := client.List(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tc.wantProbes)
		})
	}
}
