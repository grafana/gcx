package k6_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestRunThreshold_CodecRoundTrip(t *testing.T) {
	for _, body := range []string{
		`"rate<0.01"`,
		`{"threshold":"rate<0.01","abortOnFail":true,"delayAbortEval":"10s"}`,
		`{"threshold":"rate<0.01","abortOnFail":false}`,
		`{"threshold":"rate<0.01","delayAbortEval":2000}`,
		`{"threshold":"rate<0.01","delayAbortEval":null}`,
		`{"threshold":null,"abortOnFail":null}`,
		`{"threshold":"rate<0.01","futureField":{"enabled":true}}`,
		`{}`, `null`,
	} {
		t.Run(body, func(t *testing.T) {
			var threshold k6.TestRunThreshold
			require.NoError(t, json.Unmarshal([]byte(body), &threshold))
			for _, codec := range []format.Codec{format.NewJSONCodec(), format.NewYAMLCodec()} {
				t.Run(string(codec.Format()), func(t *testing.T) {
					var out bytes.Buffer
					require.NoError(t, codec.Encode(&out, threshold))
					var decoded k6.TestRunThreshold
					require.NoError(t, codec.Decode(&out, &decoded))
					encoded, err := json.Marshal(decoded)
					require.NoError(t, err)
					assert.JSONEq(t, body, string(encoded))
				})
			}
		})
	}
}

func TestTestRunThreshold_RejectsMalformedTypes(t *testing.T) {
	for _, body := range []string{
		`42`, `true`, `[]`,
		`{"threshold":42}`, `{"threshold":[]}`,
		`{"abortOnFail":"true"}`,
		`{"delayAbortEval":true}`, `{"delayAbortEval":{}}`, `{"delayAbortEval":[]}`,
		`{"delayAbortEval":1e1000}`, `{"threshold":`,
	} {
		t.Run(body, func(t *testing.T) {
			var threshold k6.TestRunThreshold
			require.Error(t, json.Unmarshal([]byte(body), &threshold))
		})
	}
}

const mixedThresholdRun = `{"id":101,"load_test_id":6,"result_status":1,"status":"completed","options":{"thresholds":{"http_req_failed":["rate<0.01",{"threshold":"rate<0.01","abortOnFail":true,"delayAbortEval":"10s"},null],"checks":null}}}`

func TestTestRunThreshold_MixedRunAndListOutput(t *testing.T) {
	var run k6.TestRunStatus
	require.NoError(t, json.Unmarshal([]byte(mixedThresholdRun), &run))
	for _, codec := range []format.Codec{format.NewJSONCodec(), format.NewYAMLCodec()} {
		t.Run(string(codec.Format()), func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, codec.Encode(&out, run))
			var decoded k6.TestRunStatus
			require.NoError(t, codec.Decode(&out, &decoded))
			encoded, err := json.Marshal(decoded)
			require.NoError(t, err)
			assert.JSONEq(t, mixedThresholdRun, string(encoded))
		})
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/cloud/v6/load_tests/6/test_runs", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[` + mixedThresholdRun + `]}`))
	})
	client := newAuthenticatedProxyClient(t, handler)
	runs, err := client.ListTestRuns(t.Context(), 6)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	encoded, err := json.Marshal(runs[0])
	require.NoError(t, err)
	assert.JSONEq(t, mixedThresholdRun, string(encoded))
}

func TestTestRunThreshold_NullOptionsCollectionsAndZeroValue(t *testing.T) {
	for _, body := range []string{
		`{"id":101,"load_test_id":6,"result_status":1,"status":"completed","options":null}`,
		`{"id":101,"load_test_id":6,"result_status":1,"status":"completed","options":{"thresholds":null}}`,
		`{"id":101,"load_test_id":6,"result_status":1,"status":"completed","options":{"thresholds":{"checks":null}}}`,
	} {
		var run k6.TestRunStatus
		require.NoError(t, json.Unmarshal([]byte(body), &run))
		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		if run.Options != nil && run.Options.Thresholds != nil {
			assert.JSONEq(t, body, string(encoded))
		}
	}
	encoded, err := json.Marshal(k6.TestRunThreshold{})
	require.NoError(t, err)
	assert.JSONEq(t, `null`, string(encoded))
}
