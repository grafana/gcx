package irm_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pirDocURL is the PIR document link the fake hook runs report.
const pirDocURL = "https://docs.google.com/document/d/abc123/edit"

// pirHookRun builds a hook run carrying a fileURL metadata field.
func pirHookRun(hookID string) map[string]any {
	return map[string]any{
		"hookID": hookID,
		"metadata": map[string]any{
			"fields": []map[string]any{
				{"key": "documentTitle", "value": "PIR: Boom"},
				{"key": "fileURL", "value": pirDocURL},
			},
		},
	}
}

// TestGetPIRURL covers resolving the PIR document link from an incident's
// integration hook runs, including the shapes that legitimately have no PIR.
func TestGetPIRURL(t *testing.T) {
	tests := []struct {
		name     string
		hookRuns []map[string]any
		want     string
	}{
		{
			name:     "google copyFile hook carries the PIR link",
			hookRuns: []map[string]any{pirHookRun("grate.google.copyFile")},
			want:     pirDocURL,
		},
		{
			name:     "google workspace copyTemplate hook carries the PIR link",
			hookRuns: []map[string]any{pirHookRun("grate.googleworkspace.copyTemplate")},
			want:     pirDocURL,
		},
		{
			name: "PIR hook is found among unrelated hook runs",
			hookRuns: []map[string]any{
				{"hookID": "grate.slack.createChannel"},
				{"hookID": "grate.zoom.createMeeting"},
				pirHookRun("grate.google.copyFile"),
			},
			want: pirDocURL,
		},
		{
			name: "falls through a PIR hook that recorded no fileURL",
			hookRuns: []map[string]any{
				{"hookID": "grate.google.copyFile", "metadata": map[string]any{
					"fields": []map[string]any{{"key": "documentTitle", "value": "PIR: Boom"}},
				}},
				pirHookRun("grate.googleworkspace.copyTemplate"),
			},
			want: pirDocURL,
		},
		{
			name:     "no hook runs at all",
			hookRuns: []map[string]any{},
			want:     "",
		},
		{
			name:     "only non-PIR hooks ran",
			hookRuns: []map[string]any{{"hookID": "grate.slack.createChannel"}},
			want:     "",
		},
		{
			name:     "PIR hook without metadata",
			hookRuns: []map[string]any{{"hookID": "grate.google.copyFile"}},
			want:     "",
		},
		{
			name: "PIR hook with an empty fileURL value",
			hookRuns: []map[string]any{{"hookID": "grate.google.copyFile", "metadata": map[string]any{
				"fields": []map[string]any{{"key": "fileURL", "value": ""}},
			}}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"hookRuns": tt.hookRuns})
			}))
			defer server.Close()

			got, err := newTestClient(t, server).GetPIRURL(t.Context(), "inc-1")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGetHookRunsRequest pins the wire contract: the incident ID rides in the
// body and IntegrationService is served from the unversioned base path.
func TestGetHookRunsRequest(t *testing.T) {
	var gotPath string
	var gotRawBody []byte

	// The handler runs on another goroutine, so it only records: assertions
	// stay on the test goroutine below.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawBody, _ = io.ReadAll(r.Body)
		writeJSON(w, map[string]any{"hookRuns": []map[string]any{}})
	}))
	defer server.Close()

	_, err := newTestClient(t, server).GetHookRuns(t.Context(), "inc-77")
	require.NoError(t, err)

	assert.Equal(t, "/api/plugins/grafana-irm-app/resources/api/IntegrationService.GetHookRuns", gotPath)
	assert.NotContains(t, gotPath, "/v1/", "IntegrationService is not part of the documented v1 API")

	var gotBody map[string]any
	require.NoError(t, json.Unmarshal(gotRawBody, &gotBody))
	assert.Equal(t, map[string]any{"incidentID": "inc-77"}, gotBody)
}

// TestGetHookRunsErrors covers the failure modes, including the in-band error
// the IRM API can return on a 200 response.
func TestGetHookRunsErrors(t *testing.T) {
	t.Run("empty incident ID is rejected before any request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("expected no HTTP call for an empty incident ID")
		}))
		defer server.Close()

		_, err := newTestClient(t, server).GetHookRuns(t.Context(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incidentID is required")
	})

	t.Run("in-band error on a 200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"error": "integration unavailable"})
		}))
		defer server.Close()

		_, err := newTestClient(t, server).GetHookRuns(t.Context(), "inc-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "integration unavailable")
	})

	t.Run("non-200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		}))
		defer server.Close()

		_, err := newTestClient(t, server).GetPIRURL(t.Context(), "inc-1")
		require.Error(t, err)
	})
}

// TestIncidentsGetPIROutputContract pins the agent output contract for
// `irm incidents get-pir`: the bare URL for humans, one JSON value for agents,
// and a missing PIR reported as a stderr diagnostic rather than an error.
func TestIncidentsGetPIROutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		args       []string
		hookRuns   []map[string]any
		wantStdout string // exact stdout when wantJSON is false
		wantJSON   bool
		wantPIR    string
		wantNote   bool // expect the "no PIR document" note on stderr
	}{
		{
			name:       "human default is the bare URL",
			args:       []string{"inc-1"},
			hookRuns:   []map[string]any{pirHookRun("grate.google.copyFile")},
			wantStdout: pirDocURL + "\n",
			wantPIR:    pirDocURL,
		},
		{
			name:       "no PIR prints nothing and notes it on stderr",
			args:       []string{"inc-1"},
			hookRuns:   []map[string]any{{"hookID": "grate.slack.createChannel"}},
			wantStdout: "",
			wantNote:   true,
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			args:      []string{"inc-1"},
			hookRuns:  []map[string]any{pirHookRun("grate.google.copyFile")},
			wantJSON:  true,
			wantPIR:   pirDocURL,
		},
		{
			name:     "explicit -o json wins outside agent mode",
			args:     []string{"inc-1", "-o", "json"},
			hookRuns: []map[string]any{pirHookRun("grate.google.copyFile")},
			wantJSON: true,
			wantPIR:  pirDocURL,
		},
		{
			name:      "agent mode reports a missing PIR as an empty pirURL",
			agentMode: true,
			args:      []string{"inc-1"},
			hookRuns:  []map[string]any{},
			wantJSON:  true,
			wantPIR:   "",
			wantNote:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentModeExt(t, tc.agentMode)
			server := incidentAPIServer(t, map[string]any{
				"IntegrationService.GetHookRuns": map[string]any{"hookRuns": tc.hookRuns},
			})

			stdout, stderr, err := runIncidentCmd(t, func() *cobra.Command {
				return irm.NewGetPIRCommand(incidentLoader(server))
			}, "", tc.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantNote {
				if !strings.Contains(stderr, "has no PIR document") {
					t.Errorf("expected the missing-PIR note on stderr; stderr=%q", stderr)
				}
			} else if strings.Contains(stderr, "has no PIR document") {
				t.Errorf("unexpected missing-PIR note on stderr; stderr=%q", stderr)
			}

			if tc.wantJSON {
				doc := decodeOneJSONValue(t, stdout)
				if doc["incidentID"] != "inc-1" {
					t.Errorf("incidentID = %v, want inc-1", doc["incidentID"])
				}
				if doc["pirURL"] != tc.wantPIR {
					t.Errorf("pirURL = %v, want %q", doc["pirURL"], tc.wantPIR)
				}
				return
			}
			if stdout != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tc.wantStdout)
			}
		})
	}
}
