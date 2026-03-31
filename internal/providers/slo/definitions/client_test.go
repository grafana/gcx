package definitions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *definitions.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := definitions.NewClient(cfg)
	require.NoError(t, err)
	return client
}

// writeJSON encodes v as JSON to w.
// Panics on marshal error since test data is always known-good.
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
		name     string
		handler  http.HandlerFunc
		wantSLOs int
		wantErr  bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo", r.URL.Path)
				writeJSON(w, definitions.SLOListResponse{
					SLOs: []definitions.Slo{
						{UUID: "uuid-1", Name: "SLO 1", Description: "First SLO"},
						{UUID: "uuid-2", Name: "SLO 2", Description: "Second SLO"},
					},
				})
			},
			wantSLOs: 2,
			wantErr:  false,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, definitions.SLOListResponse{SLOs: []definitions.Slo{}})
			},
			wantSLOs: 0,
			wantErr:  false,
		},
		{
			name: "null slos field returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, definitions.SLOListResponse{})
			},
			wantSLOs: 0,
			wantErr:  false,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, definitions.ErrorResponse{Code: 500, Error: "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			slos, err := client.List(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, slos, tt.wantSLOs)
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		handler http.HandlerFunc
		wantErr bool
		wantUID string
	}{
		{
			name: "success",
			uuid: "uuid-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/uuid-1", r.URL.Path)
				writeJSON(w, definitions.Slo{UUID: "uuid-1", Name: "SLO 1", Description: "First SLO"})
			},
			wantErr: false,
			wantUID: "uuid-1",
		},
		{
			name: "not found",
			uuid: "uuid-missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, definitions.ErrorResponse{Code: 404, Error: "SLO not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			slo, err := client.Get(t.Context(), tt.uuid)

			if tt.wantErr {
				require.Error(t, err)
				if tt.name == "not found" {
					require.ErrorIs(t, err, definitions.ErrNotFound)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, slo.UUID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name    string
		slo     *definitions.Slo
		handler http.HandlerFunc
		wantErr bool
		wantUID string
	}{
		{
			name: "success 202",
			slo:  &definitions.Slo{Name: "New SLO", Description: "A new SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var received definitions.Slo
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				assert.Equal(t, "New SLO", received.Name)

				w.WriteHeader(http.StatusAccepted)
				writeJSON(w, definitions.SLOCreateResponse{UUID: "new-uuid", Message: "SLO created"})
			},
			wantErr: false,
			wantUID: "new-uuid",
		},
		{
			name: "success 200",
			slo:  &definitions.Slo{Name: "New SLO", Description: "A new SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, definitions.SLOCreateResponse{UUID: "new-uuid-200", Message: "SLO created"})
			},
			wantErr: false,
			wantUID: "new-uuid-200",
		},
		{
			name: "400 bad request",
			slo:  &definitions.Slo{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, definitions.ErrorResponse{Code: 400, Error: "invalid SLO definition"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			resp, err := client.Create(t.Context(), tt.slo)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, resp.UUID)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		slo     *definitions.Slo
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 202",
			uuid: "uuid-1",
			slo:  &definitions.Slo{Name: "Updated SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/uuid-1", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusAccepted)
			},
			wantErr: false,
		},
		{
			name: "success 200",
			uuid: "uuid-1",
			slo:  &definitions.Slo{Name: "Updated SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
		{
			name: "not found",
			uuid: "uuid-missing",
			slo:  &definitions.Slo{Name: "Updated SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, definitions.ErrorResponse{Code: 404, Error: "SLO not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Update(t.Context(), tt.uuid, tt.slo)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 204",
			uuid: "uuid-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/uuid-1", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
			wantErr: false,
		},
		{
			name: "success 200",
			uuid: "uuid-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
		{
			name: "not found",
			uuid: "uuid-missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, definitions.ErrorResponse{Code: 404, Error: "SLO not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Delete(t.Context(), tt.uuid)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errBody    definitions.ErrorResponse
		wantErrMsg string
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			errBody:    definitions.ErrorResponse{Code: 401, Error: "unauthorized"},
			wantErrMsg: "401",
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			errBody:    definitions.ErrorResponse{Code: 403, Error: "forbidden"},
			wantErrMsg: "403",
		},
		{
			name:       "500 internal server error",
			statusCode: http.StatusInternalServerError,
			errBody:    definitions.ErrorResponse{Code: 500, Error: "internal server error"},
			wantErrMsg: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				writeJSON(w, tt.errBody)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			_, err := client.List(t.Context())

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}
