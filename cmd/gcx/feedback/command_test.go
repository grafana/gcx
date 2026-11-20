package feedback_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	feedbackcmd "github.com/grafana/gcx/cmd/gcx/feedback"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// receiver is a fake feedback endpoint that records every request.
type receiver struct {
	mu       sync.Mutex
	bodies   [][]byte
	status   int
	response string
}

func newReceiver(t *testing.T, status int, response string) *receiver {
	t.Helper()
	r := &receiver{status: status, response: response}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, body)
		r.mu.Unlock()
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
		w.WriteHeader(r.status)
		_, _ = io.WriteString(w, r.response)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GCX_FEEDBACK_ENDPOINT", srv.URL+"/gcx-feedback")
	return r
}

func (r *receiver) requests() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies
}

type wirePayload struct {
	Service    string            `json:"service"`
	FeedbackID string            `json:"feedback_id"`
	Data       map[string]string `json:"data"`
	Metadata   *struct {
		Version           string `json:"version"`
		OS                string `json:"os"`
		Arch              string `json:"arch"`
		DeviceID          string `json:"device_id"`
		DeviceIDPersisted bool   `json:"device_id_persisted"`
	} `json:"metadata"`
}

// isolate keeps the test away from the developer's config, state and
// telemetry settings.
func isolate(t *testing.T, telemetry string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GCX_CONFIG", filepath.Join(dir, "missing.yaml"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("GCX_TELEMETRY", telemetry)
	t.Setenv("DO_NOT_TRACK", "")
	return dir
}

func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := feedbackcmd.Command()
	// The gcx root sets these; without them Cobra prints usage to stdout.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func exitCode(err error) int {
	d := fail.ErrorToDetailedError(err)
	if d == nil || d.ExitCode == nil {
		return gcxerrors.ExitGeneralError
	}
	return *d.ExitCode
}

func decode(t *testing.T, body []byte) wirePayload {
	t.Helper()
	var p wirePayload
	require.NoError(t, json.Unmarshal(body, &p))
	return p
}

func TestSend_Success(t *testing.T) {
	isolate(t, "enabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	out, err := run(t, "", "send", "the error at https://example-stack.grafana.net/x was vague",
		"--field", "type=friction", "--field", "command=alerts list", "--field", "type=bug", "--field", "impact=  ")
	require.NoError(t, err)
	assert.Contains(t, out, "Feedback sent")

	reqs := r.requests()
	require.Len(t, reqs, 1)
	p := decode(t, reqs[0])
	assert.Equal(t, "gcx", p.Service)
	assert.Len(t, p.FeedbackID, 36)
	assert.Equal(t, map[string]string{
		"note":    "the error at <redacted:url> was vague",
		"type":    "bug",
		"command": "alerts list",
	}, p.Data)
	require.NotNil(t, p.Metadata)
	assert.NotEmpty(t, p.Metadata.OS)
	assert.NotEmpty(t, p.Metadata.DeviceID)
	assert.True(t, p.Metadata.DeviceIDPersisted)
}

func TestSend_JSONResult(t *testing.T) {
	isolate(t, "disabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	out, err := run(t, "", "send", "hello", "-o", "json")
	require.NoError(t, err)

	var result struct {
		Type   string `json:"type"`
		Action string `json:"action"`
		Target struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"target"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "gcx.mutation", result.Type)
	assert.Equal(t, "sent", result.Action)
	assert.Equal(t, "feedback", result.Target.Kind)
	assert.Equal(t, decode(t, r.requests()[0]).FeedbackID, result.Target.ID)
}

func TestSend_MetadataOnlyWhenTelemetryEnabled(t *testing.T) {
	tests := []struct {
		name       string
		telemetry  string
		doNotTrack string
	}{
		{name: "disabled", telemetry: "disabled"},
		{name: "log", telemetry: "log"},
		{name: "do not track", doNotTrack: "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolate(t, tc.telemetry)
			t.Setenv("DO_NOT_TRACK", tc.doNotTrack)
			r := newReceiver(t, http.StatusOK, `{"success":true}`)

			_, err := run(t, "", "send", "hello")
			require.NoError(t, err)

			reqs := r.requests()
			require.Len(t, reqs, 1)
			assert.Contains(t, string(reqs[0]), `"metadata":null`)
			assert.NoFileExists(t, filepath.Join(dir, "state", "gcx", "device-id"), "no device ID is created without telemetry")
		})
	}
}

func TestSend_DeliveryFailures(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		summary  string
		details  string
	}{
		{
			name:     "rate limited",
			status:   http.StatusTooManyRequests,
			summary:  gcxerrors.SummaryAPIError,
			details:  "Feedback could not be submitted (HTTP 429): too many requests were sent. Try again later.",
			response: "slow down",
		},
		{
			name:     "bad request",
			status:   http.StatusBadRequest,
			summary:  gcxerrors.SummaryAPIError,
			details:  "Feedback could not be submitted (HTTP 400). Try again later.",
			response: `{"success":false,"error":"bad"}`,
		},
		{
			// A 5xx can follow a stored row, so it is an unknown outcome.
			name:     "server error",
			status:   http.StatusInternalServerError,
			summary:  gcxerrors.SummaryNetworkError,
			details:  "Could not confirm that the feedback was received. It may have been submitted. gcx did not retry.",
			response: `{"success":false,"error":"storage"}`,
		},
		{
			name:     "bad gateway",
			status:   http.StatusBadGateway,
			summary:  gcxerrors.SummaryNetworkError,
			details:  "Could not confirm that the feedback was received. It may have been submitted. gcx did not retry.",
			response: "upstream error",
		},
		{
			name:     "unparseable reply",
			status:   http.StatusOK,
			summary:  gcxerrors.SummaryNetworkError,
			details:  "Could not confirm that the feedback was received. It may have been submitted. gcx did not retry.",
			response: "<html>ok</html>",
		},
		{
			name:     "reply without confirmation",
			status:   http.StatusOK,
			summary:  gcxerrors.SummaryNetworkError,
			details:  "Could not confirm that the feedback was received. It may have been submitted. gcx did not retry.",
			response: `{"success":false}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t, "disabled")
			r := newReceiver(t, tc.status, tc.response)

			out, err := run(t, "", "send", "hello")
			require.Error(t, err)
			assert.Empty(t, out)
			assert.Len(t, r.requests(), 1, "exactly one attempt, no retry")

			d := fail.ErrorToDetailedError(err)
			require.NotNil(t, d)
			assert.Equal(t, tc.summary, d.Summary)
			sent := decode(t, r.requests()[0])
			assert.Equal(t, tc.details+" Feedback ID: "+sent.FeedbackID+".", d.Details)
			assert.Equal(t, gcxerrors.ExitGeneralError, exitCode(err))
		})
	}
}

func TestSend_UnreachableEndpoint(t *testing.T) {
	isolate(t, "disabled")
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	t.Setenv("GCX_FEEDBACK_ENDPOINT", srv.URL)

	_, err := run(t, "", "send", "hello")
	d := fail.ErrorToDetailedError(err)
	require.NotNil(t, d)
	assert.Equal(t, gcxerrors.SummaryNetworkError, d.Summary)
	assert.Contains(t, d.Details, "It may have been submitted. gcx did not retry.")
}

func TestSend_DryRunMakesNoRequest(t *testing.T) {
	isolate(t, "disabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	out, err := run(t, "", "send", "my email is jane@example.com", "--field", "type=bug", "--dry-run")
	require.NoError(t, err)
	assert.Empty(t, r.requests())

	p := decode(t, []byte(out))
	assert.Equal(t, map[string]string{"note": "my email is <redacted:email>", "type": "bug"}, p.Data)
	assert.Nil(t, p.Metadata)
	assert.Contains(t, out, `"metadata": null`)
}

func TestSend_Stdin(t *testing.T) {
	isolate(t, "disabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	_, err := run(t, "line one\nline two\n", "send", "-", "--field", "type=friction")
	require.NoError(t, err)

	p := decode(t, r.requests()[0])
	assert.Equal(t, map[string]string{"note": "line one\nline two", "type": "friction"}, p.Data)
}

func TestSend_FieldOnly(t *testing.T) {
	isolate(t, "disabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	_, err := run(t, "", "send", "--field", "type=feature", "--field", "expected=a --label flag")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"type": "feature", "expected": "a --label flag"}, decode(t, r.requests()[0]).Data)
}

func TestSend_UsageErrors(t *testing.T) {
	const secretNote = "-the token is fake-secret-value"
	tests := []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "nothing given", args: []string{"send"}},
		{name: "only empty values", args: []string{"send", "  ", "--field", "type="}},
		{name: "empty stdin", args: []string{"send", "-"}},
		{name: "note given twice", args: []string{"send", "a note", "--field", "note=another"}},
		{name: "field without equals", args: []string{"send", "--field", "fake-secret-value"}},
		{name: "field with empty key", args: []string{"send", "--field", "=fake-secret-value"}},
		{name: "note parsed as flags", args: []string{"send", secretNote}},
		{name: "note parsed as a long flag", args: []string{"send", "--my token is fake-secret-value"}},
		{name: "note parsed as a long flag with value", args: []string{"send", "--my=fake-secret-value"}},
		{name: "oversized note", args: []string{"send", strings.Repeat("x", 70*1024)}},
		{name: "oversized stdin", stdin: strings.Repeat("x", 70*1024), args: []string{"send", "-"}},
		{name: "oversized after scrubbing", args: []string{"send", strings.Repeat("1.2.3.4 ", 5000)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t, "disabled")
			r := newReceiver(t, http.StatusOK, `{"success":true}`)

			out, err := run(t, tc.stdin, tc.args...)
			require.Error(t, err)
			assert.Empty(t, out)
			assert.Empty(t, r.requests(), "usage errors must not send anything")
			assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err))
			assert.NotContains(t, err.Error(), "fake-secret-value", "errors must not echo submitted text")
		})
	}
}

func TestFeedbackGroupPrintsHelpAndSendsNothing(t *testing.T) {
	isolate(t, "disabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	out, err := run(t, "")
	require.NoError(t, err)
	assert.Contains(t, out, "send")
	assert.Empty(t, r.requests())
}

func TestSend_SecretFieldValuesAreRedacted(t *testing.T) {
	isolate(t, "disabled")
	r := newReceiver(t, http.StatusOK, `{"success":true}`)

	out, err := run(t, "", "send", "--field", "password=fake-secret-value", "--field", "GRAFANA_TOKEN=fake-secret-value",
		"--field", "type=bug", "--dry-run")
	require.NoError(t, err)
	assert.Empty(t, r.requests())
	assert.Equal(t, map[string]string{
		"password":      "<redacted:secret>",
		"GRAFANA_TOKEN": "<redacted:secret>",
		"type":          "bug",
	}, decode(t, []byte(out)).Data)
}
