//nolint:testpackage // Tests the shared wire DTO and package command constructors.
package k6

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// The v6 API returns configured expressions, not evaluated threshold results.
const thresholdRunsResponse = `{"value":[{"id":101,"load_test_id":6,"result_status":1,"status":"completed","options":{"thresholds":{"checks":["rate==1"],"http_req_duration":["p(95)<1000"],"http_req_failed":["rate==0"]}}}]}`

func TestTestRunStatus_ThresholdExpressions(t *testing.T) {
	for _, tc := range []struct{ name, body, options string }{
		{"configured", thresholdRunsResponse, `{"thresholds":{"checks":["rate==1"],"http_req_duration":["p(95)<1000"],"http_req_failed":["rate==0"]}}`},
		{"absent", `{"value":[{"id":101,"status":"completed"}]}`, ""},
		{"null", `{"value":[{"id":101,"status":"completed","options":null}]}`, `null`},
		{"empty", `{"value":[{"id":101,"status":"completed","options":{"thresholds":{}}}]}`, `{"thresholds":{}}`},
		{"null thresholds", `{"value":[{"id":101,"status":"completed","options":{"thresholds":null}}]}`, `{"thresholds":null}`},
		{"other options", `{"value":[{"id":101,"status":"completed","options":{"vus":10,"duration":"30s","future":{"enabled":true}}}]}`, `{"vus":10,"duration":"30s","future":{"enabled":true}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response testRunsResponse
			require.NoError(t, json.Unmarshal([]byte(tc.body), &response))
			require.Len(t, response.Value, 1)
			encoded, err := json.Marshal(response.Value[0])
			require.NoError(t, err)
			var output map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &output))
			if tc.options == "" {
				assert.NotContains(t, output, "options")
			} else {
				assert.JSONEq(t, tc.options, string(output["options"]))
			}
		})
	}
}

func expectedThresholdExpressions() map[string]any {
	return map[string]any{
		"checks":            []any{"rate==1"},
		"http_req_duration": []any{"p(95)<1000"},
		"http_req_failed":   []any{"rate==0"},
	}
}

func TestK6RunsList_ThresholdExpressionsOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v3/account/grafana-app/start":
			_, _ = w.Write([]byte(`{"organization_id":"42","v3_grafana_token":"cached-v3"}`))
		case "/cloud/v6/load_tests/6/test_runs":
			assert.Equal(t, http.MethodGet, r.Method)
			_, _ = w.Write([]byte(thresholdRunsResponse))
		default:
			t.Errorf("Unexpected API route: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	loader := &mockLoader{
		cloudCfg:    providers.CloudRESTConfig{Stack: cloud.StackInfo{ID: 999}, Namespace: "stack-999"},
		grafanaCfg:  config.NamespacedRESTConfig{Config: rest.Config{BearerToken: "glsa_test"}},
		providerCfg: map[string]string{"api-domain": srv.URL},
	}
	for _, tc := range []struct {
		name      string
		agentMode bool
		args      []string
		selected  bool
	}{
		{"explicit JSON", false, []string{"--id", "6", "-o", "json"}, false},
		{"agent default", true, []string{"--id", "6"}, false},
		{"field selection", false, []string{"--id", "6", "--json", "id,options.thresholds"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := runK6Command(t, tc.agentMode, loader, newRunsListCommand, tc.args, "")
			require.NoError(t, err)
			var runs []map[string]any
			decoder := json.NewDecoder(strings.NewReader(stdout))
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&runs))
			require.Len(t, runs, 1)
			assert.Equal(t, json.Number("101"), runs[0]["id"])
			if tc.selected {
				assert.Equal(t, expectedThresholdExpressions(), runs[0]["options.thresholds"])
				assert.Len(t, runs[0], 2)
			} else {
				options, ok := runs[0]["options"].(map[string]any)
				require.True(t, ok, "options must be an object")
				assert.Equal(t, expectedThresholdExpressions(), options["thresholds"])
			}
		})
	}
}

func TestRunThresholdOptions_PreserveOpenValues(t *testing.T) {
	for _, thresholds := range []string{
		`{"checks":null}`,
		`{"checks":["rate==1",{"threshold":"rate<0.01","abortOnFail":"true","delayAbortEval":true,"futureField":42},null,42]}`,
	} {
		t.Run(thresholds, func(t *testing.T) {
			body := `{"load_test_id":0,"result_status":0,"status":"completed","options":{"thresholds":` + thresholds + `}}`
			var run TestRunStatus
			require.NoError(t, json.Unmarshal([]byte(body), &run))
			for _, codec := range []format.Codec{format.NewJSONCodec(), format.NewYAMLCodec()} {
				t.Run(string(codec.Format()), func(t *testing.T) {
					var out bytes.Buffer
					require.NoError(t, codec.Encode(&out, run))
					var decoded TestRunStatus
					require.NoError(t, codec.Decode(&out, &decoded))
					encoded, err := json.Marshal(decoded)
					require.NoError(t, err)
					assert.JSONEq(t, body, string(encoded))
				})
			}
		})
	}
}
