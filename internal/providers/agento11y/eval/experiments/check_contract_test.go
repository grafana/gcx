package experiments //nolint:testpackage // Tests drive the unexported emitCheckResult seam and checkOpts wiring.

// Contract tests for the emitCheckResult seam of `gcx agento11y experiments
// check`: the command writes one document to stdout on both pass and failure,
// and a failing verdict surfaces as an EmittedError carrying the partial-failure
// exit code rather than a second document.
//
// The verdict matrix lives in check_test.go and the exit-code matrix in
// cmd/gcx/root/agento11y_experiments_check_test.go. These rows cover only what
// neither reaches: per-field JSON shape, format resolution, and a broken stdout.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failWriter fails every write with err, simulating ENOSPC/EIO on stdout.
type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

func newCheckOptsForTest(t *testing.T, output string, args ...string) *checkOpts {
	t.Helper()
	opts := &checkOpts{}
	opts.IO.ErrWriter = io.Discard // silence the agent-mode --json/--jq hint
	flags := pflag.NewFlagSet("check", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse(args))
	if output != "" {
		require.NoError(t, flags.Set("output", output))
	}
	require.NoError(t, opts.Validate(nil))
	return opts
}

func completedReport(passRate *float64, passCount, passDenominator int) *ExperimentReport {
	return &ExperimentReport{
		Experiment: Experiment{ExperimentID: "r-1", Status: statusCompleted},
		Summary: ExperimentReportSummary{
			PassRate:        passRate,
			PassCount:       passCount,
			PassDenominator: passDenominator,
		},
	}
}

func TestCheck_OutputContract(t *testing.T) {
	tests := []struct {
		name       string
		report     *ExperimentReport
		args       []string
		wantErr    bool
		wantStderr []string
		wantChecks []map[string]any
	}{
		{
			// A measured check carries both numbers, and a passing verdict writes
			// the document with no diagnostic at all.
			name:   "a measured pass carries threshold and actual",
			report: completedReport(new(0.95), 19, 20),
			args:   []string{"--min-pass-rate", "0.95", "--min-verdict-coverage", "0"},
			wantChecks: []map[string]any{
				{"name": "experiment_status", "verdict": "pass"},
				{"name": "min_pass_rate", "verdict": "pass", "threshold": 0.95, "actual": 0.95},
			},
		},
		{
			// A bare invocation gates on completion and coverage alone, and the
			// document still names both assertions.
			name: "an omitted pass rate threshold emits no min_pass_rate check",
			report: &ExperimentReport{
				Experiment: Experiment{ExperimentID: "r-1", Status: statusCompleted},
				Summary: ExperimentReportSummary{
					PassRate: new(0.5), PassCount: 5, PassDenominator: 10,
					TestCaseCount: 10, CompletedCount: 10,
				},
			},
			wantChecks: []map[string]any{
				{"name": "experiment_status", "verdict": "pass"},
				{"name": "verdict_coverage", "verdict": "pass", "threshold": 1.0, "actual": 1.0},
			},
		},
		{
			// An unmeasurable check must omit actual rather than report 0, which a
			// consumer would read as a measured zero.
			name:       "an unmeasurable check omits actual",
			report:     completedReport(nil, 0, 0),
			args:       []string{"--min-pass-rate", "0.9", "--min-verdict-coverage", "0"},
			wantErr:    true,
			wantStderr: []string{"min_pass_rate", "no test case produced a pass or fail verdict"},
			wantChecks: []map[string]any{
				{"name": "experiment_status", "verdict": "pass"},
				{"name": "min_pass_rate", "verdict": "unknown", "threshold": 0.9},
			},
		},
		{
			// Both graded checks reach the document, so a CI consumer sees the
			// passing one next to the breach that failed the run.
			name: "a coverage breach emits both checks",
			report: &ExperimentReport{
				Experiment: Experiment{ExperimentID: "r-1", Status: statusCompleted},
				Summary: ExperimentReportSummary{
					PassRate: new(1.0), PassCount: 10, PassDenominator: 10,
					TestCaseCount: 100, CompletedCount: 10, FailedCount: 90,
				},
			},
			args:       []string{"--min-pass-rate", "0.9"},
			wantErr:    true,
			wantStderr: []string{"verdict_coverage", "10 of 100"},
			wantChecks: []map[string]any{
				{"name": "experiment_status", "verdict": "pass"},
				{"name": "min_pass_rate", "verdict": "pass", "threshold": 0.9, "actual": 1.0},
				{"name": "verdict_coverage", "verdict": "fail", "threshold": 1.0, "actual": 0.1},
			},
		},
		{
			// A status check has no numbers to report, so both fields stay absent.
			name: "a status check carries no numbers",
			report: &ExperimentReport{
				Experiment: Experiment{ExperimentID: "r-1", Status: statusFailed},
				Summary:    ExperimentReportSummary{PassRate: new(1.0), PassCount: 2, PassDenominator: 2},
			},
			args:       []string{"--min-pass-rate", "0.9"},
			wantErr:    true,
			wantStderr: []string{"experiment_status"},
			wantChecks: []map[string]any{
				{"name": "experiment_status", "verdict": "fail"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(true)
			t.Cleanup(agent.ResetForTesting)
			opts := newCheckOptsForTest(t, "json", tc.args...)

			var stdout, stderr bytes.Buffer
			err := emitCheckResult(&stdout, &stderr, opts, evaluateChecks(tc.report, opts.spec()))

			if tc.wantErr {
				require.Error(t, err)
				var emitted *gcxerrors.EmittedError
				require.ErrorAs(t, err, &emitted, "want EmittedError, got %T", err)
				assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
				for _, s := range tc.wantStderr {
					assert.Contains(t, stderr.String(), s)
				}
			} else {
				require.NoError(t, err)
				assert.Empty(t, stderr.String())
			}

			doc := requireOneJSONValue(t, stdout.String())
			assert.Equal(t, "gcx.agento11y.experiment_check", doc["type"], "consumers dispatch on type")
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, "r-1", doc["experiment_id"])
			assert.Equal(t, tc.report.Experiment.Status, doc["experiment_status"])
			// Zero counts must survive encoding: pass_denominator == 0 next to
			// test_case_count == 100 is the signal a whole suite was lost.
			assert.Contains(t, doc, "pass_count")
			assert.Contains(t, doc, "pass_denominator")
			assert.Contains(t, doc, "test_case_count")

			checks, ok := doc["checks"].([]any)
			require.True(t, ok, "checks must be an array: %v", doc["checks"])
			require.Len(t, checks, len(tc.wantChecks))
			for i, want := range tc.wantChecks {
				got, ok := checks[i].(map[string]any)
				require.True(t, ok, "check must be an object: %v", checks[i])
				for key, value := range want {
					assert.Equal(t, value, got[key], "check %d field %q", i, key)
				}
				assert.NotEmpty(t, got["observed"], "check %d must say what it observed", i)
				for _, key := range []string{"threshold", "actual"} {
					if _, wanted := want[key]; !wanted {
						assert.NotContains(t, got, key, "check %d must omit %s", i, key)
					}
				}
			}
		})
	}
}

// TestCheck_DefaultFormat pins format resolution: an agent gets the machine
// document with no flag, and a human gets the text rendering.
func TestCheck_DefaultFormat(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		want      []string
		notWant   []string
	}{
		{
			name:      "an agent defaults to the machine document",
			agentMode: true,
			want:      []string{"gcx.agento11y.experiment_check", "min_pass_rate"},
			notWant:   []string{"Quality check:"},
		},
		{
			name:      "a human defaults to text",
			agentMode: false,
			want:      []string{"Quality check:", "fail", "min_pass_rate"},
			notWant:   []string{"schema_version"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(agent.ResetForTesting)
			opts := newCheckOptsForTest(t, "", "--min-pass-rate", "0.9")

			var stdout, stderr bytes.Buffer
			err := emitCheckResult(&stdout, &stderr, opts, evaluateChecks(completedReport(new(0.8), 8, 10), opts.spec()))

			require.Error(t, err)
			for _, s := range tc.want {
				assert.Contains(t, stdout.String(), s)
			}
			for _, s := range tc.notWant {
				assert.NotContains(t, stdout.String(), s)
			}
			// EmittedError suppresses the reporter's own rendering, so the command
			// owes stderr a line in either mode. An agent gets it as a JSON
			// warning, a human as a warn: prefix, and both carry the same summary.
			assert.Contains(t, stderr.String(), "quality check verdict fail for experiment r-1")
		})
	}
}

// TestCheck_WriteFailureReturnsWriteError proves a broken stdout returns the
// write error, not an EmittedError: the stream is already corrupt, so the
// standard error path is the honest one.
func TestCheck_WriteFailureReturnsWriteError(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)

	writeErr := errors.New("disk full")
	opts := newCheckOptsForTest(t, "json", "--min-pass-rate", "0.9")

	var stderr bytes.Buffer
	err := emitCheckResult(failWriter{err: writeErr}, &stderr, opts, evaluateChecks(completedReport(new(0.8), 8, 10), opts.spec()))

	require.ErrorIs(t, err, writeErr)
	var emitted *gcxerrors.EmittedError
	assert.NotErrorAs(t, err, &emitted, "a failed write must not be reported as an emitted result")
	assert.Empty(t, stderr.String())
}

func TestCheckOpts_Spec(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPassRate  *float64
		wantCoverage  float64
		wantOnUnknown string
	}{
		{
			name:          "the defaults gate the whole suite and fail on an ungraded run",
			args:          []string{"--min-pass-rate", "0.75"},
			wantPassRate:  new(0.75),
			wantCoverage:  1,
			wantOnUnknown: onUnknownFail,
		},
		{
			name:          "every threshold can be overridden",
			args:          []string{"--min-pass-rate", "0.75", "--min-verdict-coverage", "0.5", "--on-unknown", "pass"},
			wantPassRate:  new(0.75),
			wantCoverage:  0.5,
			wantOnUnknown: onUnknownPass,
		},
		{
			// An omitted --min-pass-rate disables the check. An explicit zero does
			// not, so the two must resolve differently.
			name:          "an omitted pass rate threshold resolves to nil",
			args:          nil,
			wantPassRate:  nil,
			wantCoverage:  1,
			wantOnUnknown: onUnknownFail,
		},
		{
			name:          "an explicit zero pass rate threshold stays a threshold",
			args:          []string{"--min-pass-rate", "0"},
			wantPassRate:  new(0.0),
			wantCoverage:  1,
			wantOnUnknown: onUnknownFail,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := newCheckOptsForTest(t, "json", tc.args...).spec()

			if tc.wantPassRate == nil {
				assert.Nil(t, spec.MinPassRate)
			} else {
				require.NotNil(t, spec.MinPassRate)
				assert.InDelta(t, *tc.wantPassRate, *spec.MinPassRate, 1e-9)
			}
			assert.InDelta(t, tc.wantCoverage, spec.MinVerdictCoverage, 1e-9)
			assert.Equal(t, tc.wantOnUnknown, spec.OnUnknown)
		})
	}
}

func requireOneJSONValue(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF,
		"stdout must contain exactly one JSON value:\n%s", stdout)
	return doc
}
