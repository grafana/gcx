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

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name      string
		probe     probes.Probe
		handler   http.HandlerFunc
		wantID    int64
		wantToken string
		wantErr   bool
	}{
		{
			name:  "success",
			probe: probes.Probe{Name: "my-probe", Latitude: 45.0, Longitude: -122.0, Region: "US"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/v1/probe/add", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

				// Verify the body is flat JSON (not wrapped in {"probe":...})
				var body probes.Probe
				err := json.NewDecoder(r.Body).Decode(&body)
				assert.NoError(t, err)
				assert.Equal(t, "my-probe", body.Name)

				writeJSON(w, probes.CreateResponse{
					Probe: probes.Probe{ID: 99, Name: body.Name, Region: body.Region},
					Token: "probe-auth-token-abc",
				})
			},
			wantID:    99,
			wantToken: "probe-auth-token-abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := probes.NewClient(srv.URL, "test-token")
			got, err := client.Create(context.Background(), tc.probe)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.Probe.ID)
			assert.Equal(t, tc.wantToken, got.Token)
		})
	}
}

func TestClient_Create_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid probe"})
	}))
	defer srv.Close()

	client := probes.NewClient(srv.URL, "test-token")
	_, err := client.Create(context.Background(), probes.Probe{Name: "bad"})
	require.Error(t, err)
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name     string
		id       int64
		handler  http.HandlerFunc
		wantName string
		wantErr  bool
		errMsg   string
	}{
		{
			name: "found",
			id:   2,
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []probes.Probe{
					{ID: 1, Name: "Oregon"},
					{ID: 2, Name: "Paris"},
					{ID: 3, Name: "Tokyo"},
				})
			},
			wantName: "Paris",
		},
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []probes.Probe{
					{ID: 1, Name: "Oregon"},
				})
			},
			wantErr: true,
			errMsg:  "probe 999 not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := probes.NewClient(srv.URL, "test-token")
			got, err := client.Get(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, got.Name)
			assert.Equal(t, tc.id, got.ID)
		})
	}
}

func TestClient_ResetToken(t *testing.T) {
	tests := []struct {
		name    string
		probe   probes.Probe
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:  "success",
			probe: probes.Probe{ID: 42, Name: "my-probe", Region: "US"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/v1/probe/update", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				// Verify the body is flat JSON with resetToken at top level
				var body map[string]any
				err := json.NewDecoder(r.Body).Decode(&body)
				assert.NoError(t, err)
				assert.Equal(t, true, body["resetToken"])
				assert.InDelta(t, float64(42), body["id"], 0)
				assert.Equal(t, "my-probe", body["name"])

				writeJSON(w, map[string]any{
					"probe": probes.Probe{ID: 42, Name: "my-probe", Region: "US"},
				})
			},
		},
		{
			name:  "server error",
			probe: probes.Probe{ID: 1},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := probes.NewClient(srv.URL, "test-token")
			got, err := client.ResetToken(context.Background(), tc.probe)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int64(42), got.ID)
			assert.Equal(t, "my-probe", got.Name)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 200",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/v1/probe/delete/42", r.URL.Path)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				writeJSON(w, map[string]string{"msg": "ok"})
			},
		},
		{
			name: "success 204",
			id:   43,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := probes.NewClient(srv.URL, "test-token")
			err := client.Delete(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_Delete_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := probes.NewClient(srv.URL, "test-token")
	err := client.Delete(context.Background(), 999)
	require.Error(t, err)
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
