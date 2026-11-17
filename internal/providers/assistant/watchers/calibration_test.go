package watchers //nolint:testpackage // Verify command orchestration and all supported output codecs.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
)

func TestCreateCalibrationOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		status        int
		response      string
		partial       bool
	}{
		{name: "accepted", outcome: "accepted"},
		{name: "unavailable", status: 501, outcome: "unavailable"},
		{name: "denied", status: 403, outcome: "error", partial: true},
		{name: "calibration route missing", status: 404, outcome: "unavailable"},
		{name: "conflict", status: 409, outcome: "error", partial: true},
		{name: "server error", status: 500, outcome: "error", partial: true},
		{name: "invalid response", response: `{"data":{"status":"queued"}}`, outcome: "error", partial: true},
		{name: "activation contradicted", response: `{"data":{"status":"queued","startWhenReady":true}}`, outcome: "error", partial: true},
	} {
		for _, output := range []string{"json", "yaml", "agents", "text", "table", "wide"} {
			t.Run(tc.name+"/"+output, func(t *testing.T) {
				testutils.SetAgentMode(t, false)
				fixture := &watcherstest.Server{AllowWrites: true, CalibrationRequestStatus: tc.status, ErrorBody: `{"message":"SENTINEL-SECRET"}`}
				if tc.response != "" {
					fixture.CalibrationResponse = json.RawMessage(tc.response)
				}
				cmd, out, diagnostic := mutationCommand(t, mutationFixture(t, fixture), []string{"create", "-f", "-", "-o", output}, commandManifest("Checkout health", "", ""))
				err := cmd.ExecuteContext(t.Context())
				if tc.partial {
					var emitted *gcxerrors.EmittedError
					require.ErrorAs(t, err, &emitted)
					require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
				} else {
					require.NoError(t, err)
				}
				require.NotContains(t, out.String()+diagnostic.String(), "SENTINEL")
				require.Contains(t, out.String(), tc.outcome)
				require.Contains(t, out.String(), "00000000-0000-4000-8000-000000000001")
				require.Len(t, fixture.Current[0], 1, "request failure must retain the created Watcher")
				require.Equal(t, "draft", fixture.Current[0][0].Status)
				requests := fixture.MutationRequests()
				require.Len(t, requests, 2, "one create and one calibration request; no rollback or retry")
				require.Equal(t, http.MethodPost, requests[1].Method)
				require.True(t, strings.HasSuffix(requests[1].Path, "/initial-calibration"))
				require.JSONEq(t, `{"startWhenReady":false}`, string(requests[1].Body))
				if output == "text" || output == "table" || output == "wide" {
					if tc.outcome == "error" {
						require.Equal(t, 1, strings.Count(out.String(), "calibration acceptance could not be confirmed."), "recovery guidance must appear only once")
					}
					return
				}
				decoder := yaml.NewYAMLOrJSONDecoder(out, 4096)
				var result createResult
				require.NoError(t, decoder.Decode(&result))
				require.Equal(t, "gcx.mutation", result.Type)
				require.Equal(t, "created", result.Action)
				require.Equal(t, tc.outcome, result.Calibration.Outcome)
				metadata, ok := result.Resource["metadata"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, "checkout-health", metadata["name"])
				require.Equal(t, tc.partial, result.Error != "")
				require.NotEmpty(t, result.Calibration.Guidance)
				if tc.outcome == "error" {
					require.NotContains(t, result.Error, result.Calibration.Guidance)
				}
				if tc.outcome == "accepted" {
					require.Equal(t, "queued", result.Calibration.State)
					require.Contains(t, result.Calibration.Guidance, "does not establish readiness")
				}
				var extra any
				require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
			})
		}
	}
}

func TestCreateCalibration404WithMissingReadbackKeepsPartialFailure(t *testing.T) {
	testutils.SetAgentMode(t, false)
	const id = "00000000-0000-4000-8000-000000000001"
	const path = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents/" + id
	fixture := &watcherstest.Server{AllowWrites: true, CalibrationRequestStatus: http.StatusNotFound}
	var calibrationRequested atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == path+"/initial-calibration" {
			calibrationRequested.Store(true)
		}
		if r.Method == http.MethodGet && r.URL.Path == path && calibrationRequested.Load() {
			http.NotFound(w, r)
			return
		}
		fixture.ServeHTTP(w, r)
	})
	cmd, out, _ := mutationCommand(t, mutationFixture(t, handler), []string{"create", "-f", "-", "-o", "json"}, commandManifest("Checkout health", "", ""))
	err := cmd.ExecuteContext(t.Context())
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
	var result createResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "error", result.Calibration.Outcome)
	require.Equal(t, id, result.Target.ID)
	require.Contains(t, result.Error, "HTTP 404")
	require.Contains(t, result.Calibration.Guidance, "Do not rerun create")
	require.Len(t, fixture.Current[0], 1, "failed confirmation must not roll back creation")
	requests := fixture.MutationRequests()
	require.Len(t, requests, 2, "one create and one calibration POST; no rollback or retry")
	require.Equal(t, path+"/initial-calibration", requests[1].Path)
	require.JSONEq(t, `{"startWhenReady":false}`, string(requests[1].Body))
}

func TestCreatePartialConfigurationSkipsCalibration(t *testing.T) {
	for _, stage := range []string{"enrollment", "readback"} {
		t.Run(stage, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			const id = "00000000-0000-4000-8000-000000000001"
			fixture := &watcherstest.Server{AllowWrites: true}
			fields := ""
			if stage == "enrollment" {
				fixture.EnrollmentWriteStatus = map[string]int{id: 500}
				fields = "  automaticRecalibration:\n    enabled: true\n"
			} else {
				fixture.DetailStatus = map[string]int{id: 500}
			}
			t.Setenv("SENTINEL_SOURCE", "https://sentinel-secret.example.invalid/hook")
			fields += "  notifications:\n    webhook:\n      enabled: true\n      url:\n        fromEnv: SENTINEL_SOURCE\n"
			cmd, out, _ := mutationCommand(t, mutationFixture(t, fixture), []string{"create", "-f", "-"}, commandManifest("Checkout health", "", fields))
			require.Error(t, cmd.ExecuteContext(t.Context()))
			var result createResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, "not-requested", result.Calibration.Outcome)
			require.Nil(t, result.Resource)
			require.Equal(t, id, result.Target.ID)
			require.NotContains(t, out.String(), "SENTINEL")
			require.NotContains(t, out.String(), "sentinel-secret")
			for _, request := range fixture.MutationRequests() {
				require.NotContains(t, request.Path, "/initial-calibration")
			}
		})
	}
}

func TestStatusCalibrationGuidance(t *testing.T) {
	for _, tc := range []struct{ state, lifecycle string }{
		{"idle", "paused"}, {"queued", "paused"}, {"running", "paused"},
		{"completed", "paused"}, {"completed", "running"}, {"failed", "paused"},
		{"cancelled", "paused"}, {"needs_input", "paused"}, {"future-state", "paused"},
	} {
		t.Run(tc.state+"/"+tc.lifecycle, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				item := fixture()
				item.Status = tc.lifecycle
				var response any = item
				if strings.HasSuffix(r.URL.Path, "/initial-calibration") {
					response = clientwatchers.Calibration{Status: tc.state}
				}
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": response}))
			}))
			t.Cleanup(server.Close)
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			cmd, out := outputCommand(t)
			require.NoError(t, runStatus(cmd, clientwatchers.NewClient(base), &readOpts{IO: cmdio.Options{OutputFormat: "json"}, Ref: fixture().ID}))
			var result statusResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, tc.state, result.Calibration.State)
			require.NotEmpty(t, result.Calibration.Guidance)
			require.Contains(t, result.Calibration.Guidance, "Grafana", "guidance must name where to continue")
			if tc.state == "failed" || tc.state == "needs_input" {
				require.Contains(t, result.Calibration.Guidance, "do not start")
			}
			if tc.state == "completed" {
				if tc.lifecycle == "running" {
					require.Contains(t, result.Calibration.Guidance, "monitoring is already active")
					require.NotContains(t, result.Calibration.Guidance, "before explicitly starting")
				} else {
					require.Contains(t, result.Calibration.Guidance, "does not establish readiness")
				}
			}
		})
	}
}
