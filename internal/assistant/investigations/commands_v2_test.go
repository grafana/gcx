package investigations_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deprecatedCommands lists the commands newer investigations reject with 410.
var deprecatedCommands = []struct { //nolint:gochecknoglobals // shared test table
	name       string
	args       []string
	method     string
	path       string
	body       any
	action     string
	suggestion string
}{
	{name: "pause", args: []string{"pause", "inv-1"}, method: http.MethodPost, path: "/pause",
		body: investigations.Message{Message: "paused"}, action: "pausing",
		suggestion: "To stop the investigation, cancel it: gcx assistant investigations cancel inv-1"},
	{name: "resume", args: []string{"resume", "inv-1"}, method: http.MethodPost, path: "/resume",
		body: investigations.Message{Message: "resumed"}, action: "resuming",
		suggestion: "Check the investigation's current state: gcx assistant investigations get inv-1"},
	{name: "mode", args: []string{"mode", "inv-1", "low"}, method: http.MethodPut, path: "/mode",
		body: investigations.ModeResponse{Message: "updated", Mode: "low"}, action: "changing the autonomy mode",
		suggestion: "Check the investigation's current state: gcx assistant investigations get inv-1"},
}

// deprecatedHandler resolves inv-1 and answers the operation with status.
func deprecatedHandler(t *testing.T, method, opPath string, status int, body any) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == v2InvestigationsPath+"/inv-1" && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{
				"data": investigations.ResolveByIDResponse{InvestigationID: "inv-1", ChatID: "chat-1"},
			})
		case r.URL.Path == v2InvestigationsPath+"/inv-1"+opPath && r.Method == method:
			if status != http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"no longer supported for this investigation; use POST /api/v2/investigations/inv-1/cancel"}`))
				return
			}
			writeJSON(w, map[string]any{"data": body})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// setAgentMode pins agent mode; detection otherwise reads the shell's env at init.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", strconv.FormatBool(enabled))
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

func runCommand(t *testing.T, handler http.Handler, args []string) (string, string, error) {
	t.Helper()
	cmd := investigations.Commands(newGetLoader(t, handler))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestDeprecatedCommands_GoneIsFriendlyError(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	setAgentMode(t, false)

	for _, tc := range deprecatedCommands {
		t.Run(tc.name, func(t *testing.T) {
			handler := deprecatedHandler(t, tc.method, tc.path, http.StatusGone, nil)
			_, stderr, err := runCommand(t, handler, tc.args)
			require.Error(t, err)

			var detailed gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			assert.Equal(t, "Unsupported operation", detailed.Summary)
			assert.Equal(t, "Investigation inv-1 does not support "+tc.action+".", detailed.Details)
			assert.Equal(t, []string{tc.suggestion}, detailed.Suggestions)
			assert.NotContains(t, err.Error(), "/api/v2", "server's endpoint suggestion must not leak through")
			assert.NotContains(t, stderr, "deprecated", "the error already explains a 410")
		})
	}
}

func TestDeprecatedCommands_OtherErrorsUnchanged(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	setAgentMode(t, false)

	for _, tc := range deprecatedCommands {
		t.Run(tc.name, func(t *testing.T) {
			handler := deprecatedHandler(t, tc.method, tc.path, http.StatusInternalServerError, nil)
			_, _, err := runCommand(t, handler, tc.args)
			require.Error(t, err)

			statusErr, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
			require.True(t, ok, "want HTTPStatusError, got %T", err)
			assert.Equal(t, http.StatusInternalServerError, statusErr.Status)
		})
	}
}

func TestDeprecatedCommands_SuccessUnchanged(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")

	tests := []struct {
		name      string
		agentMode bool
		wantWarn  func(t *testing.T, stderr, want string)
	}{
		{
			name: "human",
			wantWarn: func(t *testing.T, stderr, want string) {
				t.Helper()
				assert.Equal(t, "warn: "+want+"\n", stderr)
			},
		},
		{
			name:      "agent",
			agentMode: true,
			wantWarn: func(t *testing.T, stderr, want string) {
				t.Helper()
				var event map[string]string
				require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stderr)), &event), "stderr must be one JSON line: %q", stderr)
				assert.Equal(t, map[string]string{"class": "warning", "summary": want}, event)
			},
		},
	}
	for _, mode := range tests {
		for _, tc := range deprecatedCommands {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				setAgentMode(t, mode.agentMode)

				handler := deprecatedHandler(t, tc.method, tc.path, http.StatusOK, tc.body)
				stdout, stderr, err := runCommand(t, handler, append(tc.args, "-o", "json"))
				require.NoError(t, err)

				want, err := json.Marshal(tc.body)
				require.NoError(t, err)
				assert.JSONEq(t, string(want), stdout)
				mode.wantWarn(t, stderr, "gcx assistant investigations "+tc.name+
					" is deprecated: newer investigations do not support "+tc.action+". The command remains available through v1.x")
			})
		}
	}
}
