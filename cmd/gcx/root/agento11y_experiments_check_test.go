package root_test

// End-to-end tests for `gcx agento11y experiments check`. They drive the real
// root command tree against a fake plugin API so the whole path is covered:
// flag validation, the single report request, the finished-status guard, the
// stdout document, and the error type the top-level reporter turns into an
// exit code.
//
// Exit codes are asserted the way cmd/gcx/main.go derives them: an
// EmittedError's Code wins, otherwise fail.ErrorToDetailedError decides.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReportServer serves one experiment report response and counts requests, so
// a test can prove the command issues exactly one report call, or none at all
// when a flag is invalid. A zero status means 200.
func newReportServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	if status == 0 {
		status = http.StatusOK
	}
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/report") {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		http.Error(w, "unexpected path "+req.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// exitCodeFor mirrors cmd/gcx/main.go reportError: EmittedError carries its
// own code, anything else goes through the converter chain and defaults to 1.
func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var emitted *gcxerrors.EmittedError
	if errors.As(err, &emitted) {
		return emitted.Code
	}
	detailed := fail.ErrorToDetailedError(err)
	if detailed == nil || detailed.ExitCode == nil {
		return 1
	}
	return *detailed.ExitCode
}

func runExperimentsCheck(t *testing.T, serverURL string, args ...string) (string, string, error) {
	t.Helper()
	home := testutils.SandboxConfigEnv(t)
	writeConfigFile(t, defaultConfigPath(home), serverURL, "token-a", 11111)

	cmd := root.NewCommandForTest("test", []providers.Provider{&agento11y.Agento11yProvider{}})
	cmd.SetArgs(append([]string{"agento11y", "experiments", "check"}, args...))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func TestExperimentsCheck_EndToEnd(t *testing.T) {
	tests := []struct {
		name         string
		status       int // HTTP status of the report response, zero means 200
		body         string
		args         []string
		wantExitCode int
		wantVerdict  string // empty means no check document is expected
		wantChecks   []string
		wantErr      string
		wantCalls    int64
	}{
		{
			name:         "pass rate above the threshold exits zero",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":20,"completed_count":20,"pass_rate":0.95,"pass_count":19,"pass_denominator":20}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: 0,
			wantVerdict:  "pass",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			name:         "pass rate below the threshold exits four",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":10,"completed_count":10,"pass_rate":0.8,"pass_count":8,"pass_denominator":10}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: gcxerrors.ExitPartialFailure,
			wantVerdict:  "fail",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			name:         "perfect pass rate over a tenth of the suite exits four",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":100,"trial_count":100,"completed_count":10,"failed_count":90,"pass_rate":1.0,"pass_count":10,"pass_denominator":10}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: gcxerrors.ExitPartialFailure,
			wantVerdict:  "fail",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			// Same run, but the caller accepts a tenth of the suite, so the only
			// graded threshold is the pass rate.
			name:         "a lowered coverage threshold accepts the same run",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":100,"trial_count":100,"completed_count":10,"failed_count":90,"pass_rate":1.0,"pass_count":10,"pass_denominator":10}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "--min-verdict-coverage", "0.1", "-o", "json"},
			wantExitCode: 0,
			wantVerdict:  "pass",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			name:         "a zero coverage threshold drops the coverage check",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":100,"completed_count":10,"failed_count":90,"pass_rate":1.0,"pass_count":10,"pass_denominator":10}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "--min-verdict-coverage", "0", "-o", "json"},
			wantExitCode: 0,
			wantVerdict:  "pass",
			wantChecks:   []string{"experiment_status", "min_pass_rate"},
			wantCalls:    1,
		},
		{
			name:         "absent pass rate exits four by default",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":10,"completed_count":10,"pass_count":0,"pass_denominator":0}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: gcxerrors.ExitPartialFailure,
			wantVerdict:  "fail",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			// A reward-only run: every trial completed and none produced a pass or
			// fail verdict, so there is nothing to grade and the caller says so.
			name:         "a reward-only run exits zero with the override",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":10,"trial_count":10,"completed_count":10,"pass_count":0,"pass_denominator":0}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "--on-unknown", "pass", "-o", "json"},
			wantExitCode: 0,
			wantVerdict:  "pass",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			// The regression this gate exists for: every trial of all 100 test cases
			// failed, which looks like a reward-only run in pass_rate alone. The
			// override must not wave it through, because no trial completed.
			name:         "a run whose every trial failed exits four despite the override",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":100,"trial_count":100,"completed_count":0,"failed_count":100,"pass_count":0,"pass_denominator":0}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "--on-unknown", "pass", "-o", "json"},
			wantExitCode: gcxerrors.ExitPartialFailure,
			wantVerdict:  "fail",
			wantChecks:   []string{"experiment_status", "min_pass_rate", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			name:         "failed experiment exits four with a status check",
			body:         `{"experiment":{"experiment_id":"r-1","status":"failed"},"summary":{"pass_rate":1,"pass_count":2,"pass_denominator":2}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: gcxerrors.ExitPartialFailure,
			wantVerdict:  "fail",
			wantChecks:   []string{"experiment_status"},
			wantCalls:    1,
		},
		{
			name:         "running experiment exits one and writes no document",
			body:         `{"experiment":{"experiment_id":"r-1","status":"running"},"summary":{"pass_rate":1,"pass_count":1,"pass_denominator":1}}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: 1,
			wantErr:      `experiment r-1 reports status "running"`,
			wantCalls:    1,
		},
		{
			// A server failure is not a quality verdict, so it must not borrow the
			// exit code of one, and no document may claim a run was graded.
			name:         "a report request failure exits one and writes no document",
			status:       http.StatusInternalServerError,
			body:         `{"message":"boom"}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: 1,
			wantCalls:    1,
		},
		{
			name:         "invalid threshold exits two without calling the API",
			body:         `{}`,
			args:         []string{"r-1", "--min-pass-rate", "1.1", "-o", "json"},
			wantExitCode: gcxerrors.ExitUsageError,
			wantErr:      "from 0 through 1",
			wantCalls:    0,
		},
		{
			name:         "invalid coverage threshold exits two without calling the API",
			body:         `{}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "--min-verdict-coverage", "2", "-o", "json"},
			wantExitCode: gcxerrors.ExitUsageError,
			wantErr:      "invalid --min-verdict-coverage value 2",
			wantCalls:    0,
		},
		{
			// Cobra owns the arity rejection, and no converter maps it to the usage
			// code, so it exits 1 like every other command in the CLI. What this row
			// pins is that the rejection happens before the report request.
			name:         "a missing run id is rejected without calling the API",
			body:         `{}`,
			args:         []string{"--min-pass-rate", "0.9", "-o", "json"},
			wantExitCode: 1,
			wantErr:      "accepts 1 arg",
			wantCalls:    0,
		},
		{
			// Without --min-pass-rate the gate is completion plus full coverage,
			// and the document still names both assertions.
			name:         "a bare invocation gates on completion and coverage",
			body:         `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":10,"completed_count":10,"pass_rate":0.2,"pass_count":2,"pass_denominator":10}}`,
			args:         []string{"r-1", "-o", "json"},
			wantExitCode: 0,
			wantVerdict:  "pass",
			wantChecks:   []string{"experiment_status", "verdict_coverage"},
			wantCalls:    1,
		},
		{
			name:         "a timeout without wait exits two without calling the API",
			body:         `{}`,
			args:         []string{"r-1", "--timeout", "1m", "-o", "json"},
			wantExitCode: gcxerrors.ExitUsageError,
			wantErr:      "--timeout needs --wait",
			wantCalls:    0,
		},
		{
			name:         "unknown on-unknown mode exits two without calling the API",
			body:         `{}`,
			args:         []string{"r-1", "--min-pass-rate", "0.9", "--on-unknown", "ignore", "-o", "json"},
			wantExitCode: gcxerrors.ExitUsageError,
			wantErr:      `invalid --on-unknown value "ignore"`,
			wantCalls:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := newReportServer(t, tc.status, tc.body)
			stdout, _, err := runExperimentsCheck(t, srv.URL, tc.args...)

			assert.Equal(t, tc.wantExitCode, exitCodeFor(err), "exit code (err: %v)", err)
			assert.Equal(t, tc.wantCalls, calls.Load(), "report requests")

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}

			if tc.wantVerdict == "" {
				assert.NotContains(t, stdout, `"verdict"`, "no check document may be written")
				return
			}

			var doc map[string]any
			dec := json.NewDecoder(strings.NewReader(stdout))
			require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", stdout)
			var second any
			require.ErrorIs(t, dec.Decode(&second), io.EOF,
				"stdout must contain exactly one JSON value:\n%s", stdout)
			assert.Equal(t, tc.wantVerdict, doc["verdict"])
			assert.Equal(t, "r-1", doc["experiment_id"])
			// A failing check writes this document in place of a gcx.error, so a
			// consumer needs the marker to tell the two apart.
			assert.Equal(t, "gcx.agento11y.experiment_check", doc["type"])
			assert.Equal(t, "1", doc["schema_version"])

			checks, ok := doc["checks"].([]any)
			require.True(t, ok, "checks must be an array: %v", doc["checks"])
			names := make([]string, 0, len(checks))
			for _, entry := range checks {
				check, isObject := entry.(map[string]any)
				require.True(t, isObject, "check must be an object: %v", entry)
				name, isString := check["name"].(string)
				require.True(t, isString, "check name must be a string: %v", check["name"])
				names = append(names, name)
			}
			assert.Equal(t, tc.wantChecks, names)
		})
	}
}

// TestExperimentsCheck_TextDefaultIsHumanReadable covers the default output
// path end to end: no -o flag, so the command renders its text codec.
func TestExperimentsCheck_TextDefaultIsHumanReadable(t *testing.T) {
	srv, _ := newReportServer(t, 0, `{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":10,"completed_count":10,"pass_rate":0.8,"pass_count":8,"pass_denominator":10}}`)
	stdout, stderr, err := runExperimentsCheck(t, srv.URL, "r-1", "--min-pass-rate", "0.9")

	require.Error(t, err)
	assert.Equal(t, gcxerrors.ExitPartialFailure, exitCodeFor(err))
	assert.Contains(t, stdout, "Quality check:")
	assert.Contains(t, stdout, "min_pass_rate")
	assert.Contains(t, stderr, "warn:")
}

// newWaitServer serves the experiment GET the --wait loop polls, one scripted
// status per request with the last one repeating, next to the report response
// the grading step fetches.
func newWaitServer(t *testing.T, statuses []string, reportBody string) (*httptest.Server, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	var getCalls, reportCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(req.URL.Path, "/report") {
			reportCalls.Add(1)
			_, _ = w.Write([]byte(reportBody))
			return
		}
		n := int(getCalls.Add(1)) - 1
		if n >= len(statuses) {
			n = len(statuses) - 1
		}
		_, _ = fmt.Fprintf(w, `{"experiment_id":"r-1","status":%q}`, statuses[n])
	}))
	t.Cleanup(srv.Close)
	return srv, &getCalls, &reportCalls
}

// TestExperimentsCheck_WaitGradesAFinishedRun covers the --wait path end to
// end: the poll sees a finished run and the command grades it in the same
// invocation. The status transition itself is pinned in the package's
// TestWaitForFinished, where the poll interval is controllable.
func TestExperimentsCheck_WaitGradesAFinishedRun(t *testing.T) {
	srv, getCalls, reportCalls := newWaitServer(t, []string{"completed"},
		`{"experiment":{"experiment_id":"r-1","status":"completed"},"summary":{"test_case_count":10,"completed_count":10,"pass_rate":0.95,"pass_count":9,"pass_denominator":10}}`)
	stdout, _, err := runExperimentsCheck(t, srv.URL, "r-1", "--wait", "--min-pass-rate", "0.9", "-o", "json")

	require.NoError(t, err)
	assert.Equal(t, int64(1), getCalls.Load(), "status polls")
	assert.Equal(t, int64(1), reportCalls.Load(), "report requests")
	assert.Contains(t, stdout, `"verdict": "pass"`)
}

// TestExperimentsCheck_WaitTimeoutExitsOne pins the CI contract of a wait that
// gave up: exit 1, not the quality verdict's 4, and no check document, so a
// consumer cannot mistake the timeout for a graded run.
func TestExperimentsCheck_WaitTimeoutExitsOne(t *testing.T) {
	srv, _, reportCalls := newWaitServer(t, []string{"running"}, `{}`)
	stdout, stderr, err := runExperimentsCheck(t, srv.URL, "r-1", "--wait", "--timeout", "100ms", "--min-pass-rate", "0.9", "-o", "json")

	require.Error(t, err)
	assert.Equal(t, 1, exitCodeFor(err))
	assert.Contains(t, err.Error(), "Timed out")
	assert.Equal(t, int64(0), reportCalls.Load(), "a timed out wait must not grade")
	assert.NotContains(t, stdout, `"verdict"`, "no check document may be written")
	assert.Contains(t, stderr, `status "running"`, "the wait reports progress")
}
