package prometheus_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCmd_RejectsInvalidStepBeforeConfig(t *testing.T) {
	tests := []struct {
		step    string
		wantErr string
	}{
		{"0s", "must be at least 1ms"},
		{"-1s", "must be at least 1ms"},
		{"500us", "invalid --step duration"},
	}
	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			cmd := dsprometheus.QueryCmd(nil)
			cmd.SetArgs([]string{"vector(1)", "--from", "2026-10-08T16:00:00Z", "--to", "2026-10-08T17:00:00Z", "--step", tt.step})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestQueryCmd_Resolution(t *testing.T) {
	tests := []struct {
		name       string
		timestamps []int64
		wantSteps  []int64
		wantWarn   bool
	}{
		{"requested step", []int64{1791475200000, 1791475201000, 1791475202000}, []int64{1000}, false},
		{"coarser step", []int64{1791475200000, 1791475230000, 1791475260000}, []int64{30000}, true},
		{"gaps", []int64{1791475200000, 1791475202000, 1791475203000}, []int64{1000, 2000}, true},
		{"single point", []int64{1791475200000}, []int64{}, false},
		{"empty", []int64{}, []int64{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured struct {
				Queries []map[string]any `json:"queries"`
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					if r.URL.Path == "/api/datasources/uid/prom-uid" {
						_, _ = w.Write([]byte(`{"uid":"prom-uid","type":"prometheus","name":"prometheus"}`))
					} else {
						http.NotFound(w, r)
					}
					return
				}
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
				values := make([]int, len(tt.timestamps))
				response := map[string]any{"results": map[string]any{"A": map[string]any{
					"frames": []any{map[string]any{
						"schema": map[string]any{"fields": []any{
							map[string]any{"name": "Time", "type": "time"},
							map[string]any{"name": "Value", "type": "number"},
						}},
						"data": map[string]any{"values": []any{tt.timestamps, values}},
					}},
				}}}
				assert.NoError(t, json.NewEncoder(w).Encode(response))
			}))
			defer server.Close()
			cfg := writeLabelsTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+server.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfg)
			cmd := dsprometheus.QueryCmd(loader)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"vector(1)", "-d", "prom-uid", "--from", "2026-10-08T16:00:00Z", "--to", "2026-10-08T17:00:00Z", "--step", "1s", "-o", "json"})
			require.NoError(t, cmd.Execute())
			require.Len(t, captured.Queries, 1)
			query := captured.Queries[0]
			assert.EqualValues(t, 1000, query["intervalMs"])
			assert.EqualValues(t, 3600, query["maxDataPoints"])
			var response prometheus.QueryResponse
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &response))
			require.NotNil(t, response.Resolution)
			assert.EqualValues(t, 1000, response.Resolution.RequestedStepMs)
			assert.Equal(t, tt.wantSteps, response.Resolution.ObservedStepMs)
			if tt.wantWarn {
				assert.Contains(t, stderr.String(), "returned timestamp spacing")
			} else {
				assert.NotContains(t, stderr.String(), "returned timestamp spacing")
			}
		})
	}
}
