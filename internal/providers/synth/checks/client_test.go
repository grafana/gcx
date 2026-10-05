package checks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const testDSUID = "sm-ds-uid"

// proxyPath is the datasource-proxy path the dual-mode client builds for a given
// logical SM API path (e.g. "check/list").
func proxyPath(smPath string) string {
	return "/api/datasources/proxy/uid/" + testDSUID + "/sm/" + smPath
}

// proxyClient returns a proxy-only client (no direct fallback) pointed at srv.
func proxyClient(t *testing.T, srv *httptest.Server) *checks.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}}
	client, err := checks.NewClient(cfg, testDSUID, nil)
	require.NoError(t, err)
	return client
}

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
		wantChecks int
		wantErr    bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, proxyPath("check/list"), r.URL.Path)
				writeJSON(w, []checks.Check{
					{ID: 1, Job: "job-1", Target: "https://example.com"},
					{ID: 2, Job: "job-2", Target: "https://example.org"},
				})
			},
			wantChecks: 2,
		},
		{
			name: "empty list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []checks.Check{})
			},
			wantChecks: 0,
		},
		{
			name: "null response returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("null"))
			},
			wantChecks: 0,
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).List(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tc.wantChecks)
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		handler http.HandlerFunc
		wantJob string
		wantErr bool
		errIs   error
	}{
		{
			name: "success",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, proxyPath("check/42"), r.URL.Path)
				writeJSON(w, checks.Check{ID: 42, Job: "my-job"})
			},
			wantJob: "my-job",
		},
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
			errIs:   checks.ErrNotFound,
		},
		{
			name: "server error",
			id:   1,
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

			got, err := proxyClient(t, srv).Get(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errIs != nil {
					require.ErrorIs(t, err, tc.errIs)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantJob, got.Job)
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name    string
		check   checks.Check
		handler http.HandlerFunc
		wantID  int64
		wantErr bool
	}{
		{
			name: "success",
			check: checks.Check{
				Job:      "new-job",
				Target:   "https://example.com",
				Channels: map[string]any{"k6": map[string]any{"id": "v2"}},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, proxyPath("check/add"), r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body checks.Check
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				assert.Equal(t, map[string]any{"k6": map[string]any{"id": "v2"}}, body.Channels)
				writeJSON(w, checks.Check{ID: 100, Job: body.Job})
			},
			wantID: 100,
		},
		{
			name:  "server error",
			check: checks.Check{Job: "bad-job"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid check"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).Create(context.Background(), tc.check)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

func TestClient_Validate(t *testing.T) {
	spec := checks.CheckSpec{
		Job:       "validate-job",
		Target:    "https://example.com",
		Frequency: 60000,
		Timeout:   3000,
		Enabled:   true,
		Settings:  checks.CheckSettings{"http": map[string]any{"method": "GET"}},
		Probes:    []string{"Paris", "Oregon"},
	}

	tests := []struct {
		name         string
		id           int64
		handler      http.HandlerFunc
		wantValid    bool
		wantFindings []checks.Finding
		wantErr      bool
		errIs        error
		errContains  string
	}{
		{
			name: "valid check",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, proxyPath("check/validate"), r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				// Probe names are sent as-is: the server resolves names or IDs.
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "validate-job", body["job"])
				assert.Equal(t, []any{"Paris", "Oregon"}, body["probes"])
				assert.NotContains(t, body, "id", "id must be omitted for a create-style validation")
				assert.NotContains(t, body, "tenantId")

				writeJSON(w, map[string]any{"valid": true, "findings": []any{}})
			},
			wantValid:    true,
			wantFindings: []checks.Finding{},
		},
		{
			name: "update sends id",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.InDelta(t, 42, body["id"], 0)
				writeJSON(w, map[string]any{"valid": true, "findings": []any{}})
			},
			wantValid:    true,
			wantFindings: []checks.Finding{},
		},
		{
			name: "422 is a result, not an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				writeJSON(w, map[string]any{
					"valid": false,
					"findings": []map[string]string{
						{"severity": "error", "field": "probes", "msg": "invalid probe identifier"},
						{"severity": "error", "field": "", "msg": "invalid check timeout"},
					},
				})
			},
			wantValid: false,
			wantFindings: []checks.Finding{
				{Severity: "error", Field: "probes", Msg: "invalid probe identifier"},
				{Severity: "error", Field: "", Msg: "invalid check timeout"},
			},
		},
		{
			name: "400 undecodable body is an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"msg": "failed to decode incoming check", "err": "bad json"})
			},
			wantErr: true,
		},
		{
			name: "500 is an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"msg": "internal error"})
			},
			wantErr: true,
		},
		{
			name: "404 means the server predates the endpoint",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
			errIs:   checks.ErrValidateUnsupported,
		},
		{
			name: "405 means the server predates the endpoint",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusMethodNotAllowed)
			},
			wantErr: true,
			errIs:   checks.ErrValidateUnsupported,
		},
		{
			name: "422 with an undecodable body is an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte("<html>gateway</html>"))
			},
			wantErr: true,
		},
		{
			name: "200 with an undecodable body is an error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("<html>gateway</html>"))
			},
			wantErr:     true,
			errContains: "decoding validation response",
		},
		{
			name: "422 without findings keeps the server message",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				writeJSON(w, map[string]string{"msg": "upstream rejected the request"})
			},
			wantErr:     true,
			errContains: "upstream rejected the request",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).Validate(context.Background(), spec, tc.id)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errIs != nil {
					require.ErrorIs(t, err, tc.errIs)
				}
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantValid, got.Valid)
			assert.Equal(t, tc.wantFindings, got.Findings)
		})
	}
}

func TestValidateResult_Error(t *testing.T) {
	tests := []struct {
		name    string
		result  checks.ValidateResult
		wantErr string // empty means nil error
	}{
		{name: "valid", result: checks.ValidateResult{Valid: true}},
		{
			name: "warnings only do not fail",
			result: checks.ValidateResult{Valid: true, Findings: []checks.Finding{
				{Severity: "warning", Field: "frequency", Msg: "below the app minimum"},
			}},
		},
		{
			name: "unknown severity with valid true does not fail",
			result: checks.ValidateResult{Valid: true, Findings: []checks.Finding{
				{Severity: "info", Field: "frequency", Msg: "FYI"},
			}},
		},
		{
			name: "unknown severity with valid false falls back to the generic error",
			result: checks.ValidateResult{Valid: false, Findings: []checks.Finding{
				{Severity: "info", Field: "frequency", Msg: "FYI"},
			}},
			wantErr: "server reported the check as invalid",
		},
		{
			name: "field and message",
			result: checks.ValidateResult{Findings: []checks.Finding{
				{Severity: "error", Field: "probes", Msg: "invalid probe identifier"},
			}},
			wantErr: "probes: invalid probe identifier",
		},
		{
			name: "empty field renders message only",
			result: checks.ValidateResult{Findings: []checks.Finding{
				{Severity: "error", Msg: "invalid check timeout"},
			}},
			wantErr: "invalid check timeout",
		},
		{
			name: "multiple errors, warnings excluded",
			result: checks.ValidateResult{Findings: []checks.Finding{
				{Severity: "error", Field: "probes", Msg: "invalid probe identifier"},
				{Severity: "warning", Field: "frequency", Msg: "below the app minimum"},
				{Severity: "error", Msg: "invalid check timeout"},
			}},
			wantErr: "probes: invalid probe identifier\ninvalid check timeout",
		},
		{
			name:    "valid false with no findings still fails",
			result:  checks.ValidateResult{Valid: false},
			wantErr: "server reported the check as invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.result.Error()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestClient_RunAdhoc(t *testing.T) {
	tests := []struct {
		name    string
		req     checks.AdHocCheckRequest
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "success",
			req: checks.AdHocCheckRequest{
				Timeout:  3000,
				Settings: checks.CheckSettings{"http": map[string]any{}},
				Probes:   []int64{1, 2},
				Target:   "https://example.com",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, proxyPath("check/adhoc"), r.URL.Path)
				var body checks.AdHocCheckRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, []int64{1, 2}, body.Probes)
				assert.Equal(t, "https://example.com", body.Target)
				writeJSON(w, checks.AdHocCheckResponse{
					ID:       "3fa85f64-5717-4562-b3fc-2c963f66afa6",
					TenantID: 214,
					Timeout:  3000,
					Settings: checks.CheckSettings{"http": map[string]any{}},
					Probes:   []int64{1, 2},
					Target:   "https://example.com",
				})
			},
			wantID: "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		},
		{
			name: "server error",
			req:  checks.AdHocCheckRequest{Target: "https://bad.example.com"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid target"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			got, err := proxyClient(t, srv).RunAdhoc(context.Background(), tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name    string
		check   checks.Check
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			check: checks.Check{
				ID:       42,
				TenantID: 1,
				Job:      "updated-job",
				Channels: map[string]any{"k6": map[string]any{"id": "v2"}},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, proxyPath("check/update"), r.URL.Path)
				var body checks.Check
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				assert.Equal(t, int64(42), body.ID)
				assert.Equal(t, map[string]any{"k6": map[string]any{"id": "v2"}}, body.Channels)
				writeJSON(w, body)
			},
		},
		{
			name:  "server error",
			check: checks.Check{ID: 1},
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

			_, err := proxyClient(t, srv).Update(context.Background(), tc.check)
			if tc.wantErr {
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
		id      int64
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 200",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, proxyPath("check/delete/42"), r.URL.Path)
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
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			err := proxyClient(t, srv).Delete(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_GetTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, proxyPath("tenant"), r.URL.Path)
		writeJSON(w, checks.Tenant{ID: 214})
	}))
	defer srv.Close()

	tenant, err := proxyClient(t, srv).GetTenant(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(214), tenant.ID)
}

func TestClient_ListProbes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, proxyPath("probe/list"), r.URL.Path)
		writeJSON(w, []map[string]any{
			{"id": 1, "name": "Oregon"},
			{"id": 2, "name": "Paris"},
		})
	}))
	defer srv.Close()

	probes, err := proxyClient(t, srv).ListProbes(context.Background())
	require.NoError(t, err)
	require.Len(t, probes, 2)
	assert.Equal(t, int64(1), probes[0].ID)
	assert.Equal(t, "Oregon", probes[0].Name)
}
