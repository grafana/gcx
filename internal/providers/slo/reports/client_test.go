package reports_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *reports.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := reports.NewClient(cfg)
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
		name        string
		handler     http.HandlerFunc
		wantReports int
		wantErr     bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/report", r.URL.Path)
				writeJSON(w, reports.ReportListResponse{
					Reports: []reports.Report{
						{UUID: "uuid-1", Name: "Report 1", Description: "First report"},
						{UUID: "uuid-2", Name: "Report 2", Description: "Second report"},
					},
				})
			},
			wantReports: 2,
			wantErr:     false,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, reports.ReportListResponse{Reports: []reports.Report{}})
			},
			wantReports: 0,
			wantErr:     false,
		},
		{
			name: "null reports field returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, reports.ReportListResponse{})
			},
			wantReports: 0,
			wantErr:     false,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, reports.ErrorResponse{Code: 500, Error: "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rpts, err := client.List(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, rpts, tt.wantReports)
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
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/report/uuid-1", r.URL.Path)
				writeJSON(w, reports.Report{UUID: "uuid-1", Name: "Report 1", Description: "First report"})
			},
			wantErr: false,
			wantUID: "uuid-1",
		},
		{
			name: "not found",
			uuid: "uuid-missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, reports.ErrorResponse{Code: 404, Error: "report not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			report, err := client.Get(t.Context(), tt.uuid)

			if tt.wantErr {
				require.Error(t, err)
				if tt.name == "not found" {
					require.ErrorIs(t, err, reports.ErrNotFound)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, report.UUID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name    string
		report  *reports.Report
		handler http.HandlerFunc
		wantErr bool
		wantUID string
	}{
		{
			name: "success 202",
			report: &reports.Report{
				Name:        "New Report",
				Description: "A new report",
				TimeSpan:    "calendarMonth",
				ReportDefinition: reports.ReportDefinition{
					Slos: []reports.ReportSlo{{SloUUID: "slo-uuid-1"}},
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var received reports.Report
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				assert.Equal(t, "New Report", received.Name)

				w.WriteHeader(http.StatusAccepted)
				writeJSON(w, reports.ReportCreateResponse{UUID: "new-uuid", Message: "Report created"})
			},
			wantErr: false,
			wantUID: "new-uuid",
		},
		{
			name: "success 200",
			report: &reports.Report{
				Name:        "New Report",
				Description: "A new report",
				TimeSpan:    "calendarMonth",
				ReportDefinition: reports.ReportDefinition{
					Slos: []reports.ReportSlo{{SloUUID: "slo-uuid-1"}},
				},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, reports.ReportCreateResponse{UUID: "new-uuid-200", Message: "Report created"})
			},
			wantErr: false,
			wantUID: "new-uuid-200",
		},
		{
			name:   "400 bad request",
			report: &reports.Report{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, reports.ErrorResponse{Code: 400, Error: "invalid report definition"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			resp, err := client.Create(t.Context(), tt.report)

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
		report  *reports.Report
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:   "success 202",
			uuid:   "uuid-1",
			report: &reports.Report{Name: "Updated Report"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/report/uuid-1", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusAccepted)
			},
			wantErr: false,
		},
		{
			name:   "success 200",
			uuid:   "uuid-1",
			report: &reports.Report{Name: "Updated Report"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
		{
			name:   "not found",
			uuid:   "uuid-missing",
			report: &reports.Report{Name: "Updated Report"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, reports.ErrorResponse{Code: 404, Error: "report not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Update(t.Context(), tt.uuid, tt.report)

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
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/report/uuid-1", r.URL.Path)
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
				writeJSON(w, reports.ErrorResponse{Code: 404, Error: "report not found"})
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
		errBody    reports.ErrorResponse
		wantErrMsg string
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			errBody:    reports.ErrorResponse{Code: 401, Error: "unauthorized"},
			wantErrMsg: "401",
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			errBody:    reports.ErrorResponse{Code: 403, Error: "forbidden"},
			wantErrMsg: "403",
		},
		{
			name:       "500 internal server error",
			statusCode: http.StatusInternalServerError,
			errBody:    reports.ErrorResponse{Code: 500, Error: "internal server error"},
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
