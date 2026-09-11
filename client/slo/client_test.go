package slo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	slo "github.com/grafana/gcx/client/slo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(server *httptest.Server) *slo.Client {
	return slo.NewClient(server.Client(), server.URL)
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
				writeJSON(w, map[string]any{
					"slos": []map[string]any{
						{"uuid": "uuid-1", "name": "SLO 1", "description": "First SLO"},
						{"uuid": "uuid-2", "name": "SLO 2", "description": "Second SLO"},
					},
				})
			},
			wantSLOs: 2,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"slos": []map[string]any{}})
			},
			wantSLOs: 0,
		},
		{
			name: "null slos field returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{})
			},
			wantSLOs: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(server)
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
				writeJSON(w, map[string]any{"uuid": "uuid-1", "name": "SLO 1", "description": "First SLO"})
			},
			wantUID: "uuid-1",
		},
		{
			name: "not found",
			uuid: "uuid-missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]string{"error": "SLO not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(server)
			got, err := client.Get(t.Context(), tt.uuid)

			if tt.wantErr {
				require.Error(t, err)
				if tt.name == "not found" {
					require.ErrorIs(t, err, slo.ErrNotFound)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, got.UUID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name    string
		slo     *slo.Slo
		handler http.HandlerFunc
		wantErr bool
		wantUID string
	}{
		{
			name: "success 202 then fetch",
			slo:  &slo.Slo{Name: "New SLO", Description: "A new SLO"},
			handler: func() http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodPost:
						assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
						var received slo.Slo
						if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						assert.Equal(t, "New SLO", received.Name)
						w.WriteHeader(http.StatusAccepted)
						writeJSON(w, map[string]string{"uuid": "new-uuid", "message": "SLO created"})
					case http.MethodGet:
						assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/new-uuid", r.URL.Path)
						writeJSON(w, map[string]any{"uuid": "new-uuid", "name": "New SLO"})
					default:
						w.WriteHeader(http.StatusMethodNotAllowed)
					}
				}
			}(),
			wantUID: "new-uuid",
		},
		{
			name: "400 bad request",
			slo:  &slo.Slo{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid SLO definition"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(server)
			got, err := client.Create(t.Context(), tt.slo)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, got.UUID)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		slo     *slo.Slo
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 202 then fetch",
			uuid: "uuid-1",
			slo:  &slo.Slo{Name: "Updated SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPut:
					assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/uuid-1", r.URL.Path)
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					w.WriteHeader(http.StatusAccepted)
				case http.MethodGet:
					writeJSON(w, map[string]any{"uuid": "uuid-1", "name": "Updated SLO"})
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			},
		},
		{
			name: "not found",
			uuid: "uuid-missing",
			slo:  &slo.Slo{Name: "Updated SLO"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]string{"error": "SLO not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(server)
			got, err := client.Update(t.Context(), tt.uuid, tt.slo)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.uuid, got.UUID)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name      string
		uuid      string
		confirmed bool
		handler   http.HandlerFunc
		wantErr   bool
		wantErrIs error
	}{
		{
			name:      "success 204",
			uuid:      "uuid-1",
			confirmed: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/uuid-1", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name:      "success 200",
			uuid:      "uuid-1",
			confirmed: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:      "not found",
			uuid:      "uuid-missing",
			confirmed: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]string{"error": "SLO not found"})
			},
			wantErr: true,
		},
		{
			name:      "not confirmed never hits the server",
			uuid:      "uuid-1",
			confirmed: false,
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("server must not be called when confirmed is false")
			},
			wantErr:   true,
			wantErrIs: slo.ErrDeleteNotConfirmed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(server)
			err := client.Delete(t.Context(), tt.uuid, tt.confirmed)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}
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
		errBody    map[string]string
		wantErrMsg string
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			errBody:    map[string]string{"error": "unauthorized"},
			wantErrMsg: "401",
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			errBody:    map[string]string{"error": "forbidden"},
			wantErrMsg: "403",
		},
		{
			name:       "500 internal server error",
			statusCode: http.StatusInternalServerError,
			errBody:    map[string]string{"error": "internal server error"},
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

			client := newTestClient(server)
			_, err := client.List(t.Context())

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)

			var httpErr interface{ HTTPStatusCode() int }
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, tt.statusCode, httpErr.HTTPStatusCode())
		})
	}
}
