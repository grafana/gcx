package experiments //nolint:testpackage // Tests drive the unexported evaluateChecks, checkStatusError and checkFailureSummary helpers.

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func checkReport(status string, summary ExperimentReportSummary) *ExperimentReport {
	return &ExperimentReport{
		Experiment: Experiment{ExperimentID: "r-1", Status: status},
		Summary:    summary,
	}
}

// wantCheck is the expectation for one entry of CheckResult.Checks.
type wantCheck struct {
	name      string
	verdict   CheckVerdict
	threshold *float64
	actual    *float64
	// observed substrings the diagnostic must name. Every check must set a
	// non-empty Observed, which the loop asserts separately.
	observed []string
}

// checkCase is one row of the evaluateChecks tables below. The tables are split
// by the check under test, and every row lists every check it expects, in order,
// so no row can leave a second check unasserted.
type checkCase struct {
	name        string
	report      *ExperimentReport
	spec        checkSpec
	wantVerdict CheckVerdict
	wantChecks  []wantCheck
}

func runCheckCases(t *testing.T, cases []checkCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := evaluateChecks(tc.report, tc.spec)

			assert.Equal(t, tc.wantVerdict, result.Verdict)
			assert.Equal(t, tc.report.Summary.TestCaseCount, result.TestCaseCount)
			assert.Equal(t, tc.report.Summary.PassCount, result.PassCount)
			assert.Equal(t, tc.report.Summary.PassDenominator, result.PassDenominator)

			require.Len(t, result.Checks, len(tc.wantChecks), "checks: %+v", result.Checks)
			for i, want := range tc.wantChecks {
				got := result.Checks[i]
				assert.Equal(t, want.name, got.Name)
				assert.Equal(t, want.verdict, got.Verdict, "verdict of %s", want.name)
				assert.NotEmpty(t, got.Observed, "every check must say what it observed")
				for _, s := range want.observed {
					assert.Contains(t, got.Observed, s, "observed of %s", want.name)
				}
				assertRate(t, want.threshold, got.Threshold, "threshold of "+want.name)
				assertRate(t, want.actual, got.Actual, "actual of "+want.name)
			}
		})
	}
}

// TestEvaluateChecks_MinPassRate grades the pass rate alone, so every row here
// disables the coverage check.
func TestEvaluateChecks_MinPassRate(t *testing.T) {
	runCheckCases(t, []checkCase{
		{
			name: "pass rate above threshold passes",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.95), PassCount: 19, PassDenominator: 20,
			}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictPass,
				threshold: new(0.9), actual: new(0.95),
				observed: []string{"19 of 20 graded test cases passed", "95.00%"},
			}},
		},
		{
			name: "pass rate exactly at threshold passes",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.9), PassCount: 9, PassDenominator: 10,
			}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictPass,
				threshold: new(0.9), actual: new(0.9),
			}},
		},
		{
			name: "pass rate below threshold fails",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.8), PassCount: 8, PassDenominator: 10,
			}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictFail,
				threshold: new(0.9), actual: new(0.8),
			}},
		},
		{
			name: "measured zero pass rate fails rather than reading as unknown",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.0), PassCount: 0, PassDenominator: 3,
			}),
			spec:        checkSpec{MinPassRate: 0.1},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictFail,
				threshold: new(0.1), actual: new(0.0),
			}},
		},
		{
			// --min-pass-rate 0 gates on nothing, so any measured rate clears it.
			name: "a zero threshold accepts a measured zero",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.0), PassCount: 0, PassDenominator: 3,
			}),
			spec:        checkSpec{MinPassRate: 0},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictPass,
				threshold: new(0.0), actual: new(0.0),
			}},
		},
		{
			name:        "nil pass rate is unknown and fails by default",
			report:      checkReport("completed", ExperimentReportSummary{}),
			spec:        checkSpec{MinPassRate: 0.9, OnUnknown: onUnknownFail},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictUnknown, threshold: new(0.9),
				observed: []string{"no test case produced a pass or fail verdict"},
			}},
		},
		{
			name:        "nil pass rate passes with the pass override",
			report:      checkReport("completed", ExperimentReportSummary{}),
			spec:        checkSpec{MinPassRate: 0.9, OnUnknown: onUnknownPass},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictUnknown, threshold: new(0.9),
			}},
		},
		{
			name:        "empty OnUnknown defaults to fail",
			report:      checkReport("completed", ExperimentReportSummary{}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictUnknown, threshold: new(0.9),
			}},
		},
		{
			// A server older than the pass-count fields sends a rate with no
			// counts. The rate is the measurement, so it is graded.
			name: "pass rate without counts is graded",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.95),
			}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictPass,
				threshold: new(0.9), actual: new(0.95),
				observed: []string{"the server reported no pass counts"},
			}},
		},
	})
}

// TestEvaluateChecks_VerdictCoverage grades the share of the suite that produced
// a verdict, which is the blind spot pass_rate alone cannot see.
func TestEvaluateChecks_VerdictCoverage(t *testing.T) {
	runCheckCases(t, []checkCase{
		{
			name: "full verdict coverage passes",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 10, PassDenominator: 10,
				TestCaseCount: 10, CompletedCount: 10,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictPass, threshold: new(0.9), actual: new(1.0)},
				{
					name: checkNameVerdictCoverage, verdict: CheckVerdictPass,
					threshold: new(1.0), actual: new(1.0),
					observed: []string{"10 of 10 test cases produced a verdict", "100.00%"},
				},
			},
		},
		{
			// Every trial failed for 90 test cases, so only the surviving 10 were
			// graded and all of them passed.
			name: "partial verdict coverage fails a perfect pass rate",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 10, PassDenominator: 10,
				TestCaseCount: 100, TrialCount: 100, CompletedCount: 10, FailedCount: 85, CanceledCount: 5,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictPass, threshold: new(0.9), actual: new(1.0)},
				{
					name: checkNameVerdictCoverage, verdict: CheckVerdictFail,
					threshold: new(1.0), actual: new(0.1),
					observed: []string{"10 of 100 test cases produced a verdict", "10.00%", "90 trials failed or were canceled"},
				},
			},
		},
		{
			name: "both thresholds can fail at once",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.5), PassCount: 5, PassDenominator: 10,
				TestCaseCount: 100, CompletedCount: 10, FailedCount: 90,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictFail, threshold: new(0.9), actual: new(0.5)},
				{name: checkNameVerdictCoverage, verdict: CheckVerdictFail, threshold: new(1.0), actual: new(0.1)},
			},
		},
		{
			name: "partial verdict coverage passes under a lowered threshold",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 8, PassDenominator: 8,
				TestCaseCount: 10, CompletedCount: 8, FailedCount: 2,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 0.8},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictPass, threshold: new(0.9), actual: new(1.0)},
				{name: checkNameVerdictCoverage, verdict: CheckVerdictPass, threshold: new(0.8), actual: new(0.8)},
			},
		},
		{
			// An inconsistent payload can report more graded test cases than the
			// suite holds. Coverage above the threshold still passes.
			name: "coverage above one passes",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 12, PassDenominator: 12,
				TestCaseCount: 10, CompletedCount: 12,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictPass, threshold: new(0.9), actual: new(1.0)},
				{name: checkNameVerdictCoverage, verdict: CheckVerdictPass, threshold: new(1.0), actual: new(1.2)},
			},
		},
		{
			name: "zero coverage threshold skips the coverage check",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 1, PassDenominator: 1, TestCaseCount: 100,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 0},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictPass, threshold: new(0.9), actual: new(1.0)},
			},
		},
		{
			// The gate the caller asked for stays visible even when it cannot be
			// measured, so --on-unknown decides it instead of it vanishing.
			name:        "a missing suite size makes coverage unknown, not absent",
			report:      checkReport("completed", ExperimentReportSummary{PassRate: new(1.0), PassCount: 2, PassDenominator: 2}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1, OnUnknown: onUnknownFail},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictPass, threshold: new(0.9), actual: new(1.0)},
				{
					name: checkNameVerdictCoverage, verdict: CheckVerdictUnknown, threshold: new(1.0),
					observed: []string{"no test case count"},
				},
			},
		},
		{
			// Trials completed and still produced no verdict, so the evaluators
			// emit only reward or numeric scores. Coverage of a verdict nothing
			// produces measures nothing, so --on-unknown decides the run.
			name: "a reward-only run is unknown on both checks",
			report: checkReport("completed", ExperimentReportSummary{
				TestCaseCount: 10, TrialCount: 10, CompletedCount: 10, PassDenominator: 0,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1, OnUnknown: onUnknownPass},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictUnknown, threshold: new(0.9)},
				{
					name: checkNameVerdictCoverage, verdict: CheckVerdictUnknown, threshold: new(1.0),
					observed: []string{"10 trials completed and none produced a pass or fail verdict"},
				},
			},
		},
		{
			// The run --on-unknown=pass must not wave through: nothing completed,
			// so the missing pass rate is lost work rather than a reward-only
			// scoring choice, and coverage is a measured 0%.
			name: "a run whose every trial failed fails even with the pass override",
			report: checkReport("completed", ExperimentReportSummary{
				TestCaseCount: 100, TrialCount: 100, CompletedCount: 0, FailedCount: 100,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1, OnUnknown: onUnknownPass},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{
				{name: checkNameMinPassRate, verdict: CheckVerdictUnknown, threshold: new(0.9)},
				{
					name: checkNameVerdictCoverage, verdict: CheckVerdictFail,
					threshold: new(1.0), actual: new(0.0),
					observed: []string{"0 of 100 test cases produced a verdict", "100 trials failed or were canceled"},
				},
			},
		},
	})
}

// TestEvaluateChecks_Status grades the experiment status, which short-circuits
// every threshold: a run that died halfway must not pass because the trials that
// did execute happened to clear it.
func TestEvaluateChecks_Status(t *testing.T) {
	runCheckCases(t, []checkCase{
		{
			// The server normalizes status, but so does this function, because a
			// padded value must not slip past and fail a healthy run with exit 4.
			name: "status case and padding are tolerated",
			report: checkReport(" Completed ", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 2, PassDenominator: 2,
			}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictPass,
			wantChecks: []wantCheck{{
				name: checkNameMinPassRate, verdict: CheckVerdictPass,
				threshold: new(0.9), actual: new(1.0),
			}},
		},
		{
			name: "failed status fails regardless of the pass rate",
			report: checkReport("failed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 2, PassDenominator: 2,
			}),
			spec:        checkSpec{MinPassRate: 0.9},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{{
				name: checkNameExperimentStatus, verdict: CheckVerdictFail,
				observed: []string{`"failed"`, `"completed"`},
			}},
		},
		{
			name: "canceled status fails even with the unknown override",
			report: checkReport("canceled", ExperimentReportSummary{
				PassRate: new(1.0), PassDenominator: 2,
			}),
			spec:        checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1, OnUnknown: onUnknownPass},
			wantVerdict: CheckVerdictFail,
			wantChecks: []wantCheck{{
				name: checkNameExperimentStatus, verdict: CheckVerdictFail,
			}},
		},
	})
}

func TestEvaluateChecks_Identity(t *testing.T) {
	tests := []struct {
		name        string
		report      *ExperimentReport
		wantID      string
		wantStatus  string
		wantVerdict CheckVerdict
	}{
		{
			name:        "the experiment field carries the identity",
			report:      checkReport("completed", ExperimentReportSummary{PassRate: new(0.5), PassCount: 1, PassDenominator: 2}),
			wantID:      "r-1",
			wantStatus:  "completed",
			wantVerdict: CheckVerdictPass,
		},
		{
			name: "an older payload falls back to the run field",
			report: &ExperimentReport{
				Run:     Experiment{RunID: "r-legacy", Status: "completed"},
				Summary: ExperimentReportSummary{PassRate: new(0.5), PassCount: 1, PassDenominator: 2},
			},
			wantID:      "r-legacy",
			wantStatus:  "completed",
			wantVerdict: CheckVerdictPass,
		},
		{
			// Defence in depth: the command never reaches here with a nil report,
			// and an empty status must still fail rather than grade nothing.
			name:        "a nil report fails on status",
			report:      nil,
			wantVerdict: CheckVerdictFail,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := evaluateChecks(tc.report, checkSpec{MinPassRate: 0.5})

			assert.Equal(t, tc.wantID, result.ExperimentID)
			assert.Equal(t, tc.wantStatus, result.ExperimentStatus)
			assert.Equal(t, tc.wantVerdict, result.Verdict)
			// Consumers dispatch on these, so every result carries them.
			assert.Equal(t, "gcx.aio11y.experiment_check", result.Type)
			assert.Equal(t, "1", result.SchemaVersion)
		})
	}
}

func TestEvaluateChecks_ThresholdIsCopiedNotAliased(t *testing.T) {
	spec := checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1}
	result := evaluateChecks(checkReport("completed", ExperimentReportSummary{
		PassRate: new(0.95), PassCount: 19, PassDenominator: 20, TestCaseCount: 20, CompletedCount: 20,
	}), spec)

	for _, c := range result.Checks {
		require.NotNil(t, c.Threshold)
		*c.Threshold = 0
	}
	assert.InDelta(t, 0.9, spec.MinPassRate, 1e-9, "a check must not alias the caller's spec")
	assert.InDelta(t, 1.0, spec.MinVerdictCoverage, 1e-9, "a check must not alias the caller's spec")
}

func TestCheckStatusError(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr []string
	}{
		{name: "completed can be graded", status: "completed"},
		{name: "failed can be graded", status: "failed"},
		{name: "canceled can be graded", status: "canceled"},
		{name: "status case and padding are tolerated", status: " COMPLETED "},
		{
			name:    "running cannot be graded",
			status:  "running",
			wantErr: []string{"r-1", `"running"`},
		},
		{
			name:    "unrecognized status cannot be graded",
			status:  "queued",
			wantErr: []string{"r-1", `"queued"`},
		},
		{
			name:    "missing status cannot be graded",
			status:  "",
			wantErr: []string{"r-1", "(none)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkStatusError("r-1", tc.status)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, s := range tc.wantErr {
				assert.Contains(t, err.Error(), s)
			}

			var detailed *gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			// The summary is fixed so an agent can dispatch on it, and the run id
			// lives in the details instead.
			assert.Equal(t, "Experiment has not finished", detailed.Summary)
			require.NotEmpty(t, detailed.Suggestions, "an unfinished run needs a way forward")
			assert.Contains(t, detailed.Suggestions[0], "gcx agento11y experiments get r-1")
			// A nil exit code means 1. Exit 4 is the failing quality verdict, so
			// "cannot grade yet" must not borrow it.
			assert.Nil(t, detailed.ExitCode)
			var emitted *gcxerrors.EmittedError
			assert.NotErrorAs(t, err, &emitted, "no document was written, so nothing was emitted")
		})
	}
}

func TestCheckFailureSummary(t *testing.T) {
	tests := []struct {
		name    string
		report  *ExperimentReport
		spec    checkSpec
		want    []string
		notWant []string
	}{
		{
			name: "threshold breach names both numbers",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(0.8), PassCount: 8, PassDenominator: 10,
			}),
			spec: checkSpec{MinPassRate: 0.9},
			want: []string{"r-1", "min_pass_rate", "80.00%", "90.00%"},
		},
		{
			name:   "unknown verdict names the missing verdict",
			report: checkReport("completed", ExperimentReportSummary{}),
			spec:   checkSpec{MinPassRate: 0.9},
			want:   []string{"r-1", "min_pass_rate", "unknown", "no test case produced a pass or fail verdict"},
		},
		{
			// min_pass_rate passes here, so the diagnostic must name only the
			// check that failed.
			name: "coverage breach names the covered share and omits the passing check",
			report: checkReport("completed", ExperimentReportSummary{
				PassRate: new(1.0), PassCount: 10, PassDenominator: 10,
				TestCaseCount: 100, CompletedCount: 10, FailedCount: 90,
			}),
			spec:    checkSpec{MinPassRate: 0.9, MinVerdictCoverage: 1},
			want:    []string{"r-1", "verdict_coverage", "10.00%", "10 of 100"},
			notWant: []string{"min_pass_rate"},
		},
		{
			name:   "failed status names the status",
			report: checkReport("failed", ExperimentReportSummary{}),
			spec:   checkSpec{MinPassRate: 0.9},
			want:   []string{"r-1", "experiment_status", "failed"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary := checkFailureSummary(evaluateChecks(tc.report, tc.spec))
			for _, s := range tc.want {
				assert.Contains(t, summary, s)
			}
			for _, s := range tc.notWant {
				assert.NotContains(t, summary, s)
			}
		})
	}
}

func TestCheckFailureSummary_UnknownExperimentID(t *testing.T) {
	summary := checkFailureSummary(CheckResult{Verdict: CheckVerdictFail})
	assert.Contains(t, summary, "(unknown)")
	// A result carrying no check still has to produce a readable line rather than
	// one that ends in a colon.
	assert.Contains(t, summary, "no check reported a reason")
}

func TestOnUnknown(t *testing.T) {
	tests := []struct {
		mode        string
		wantErr     bool
		wantVerdict CheckVerdict
	}{
		{mode: onUnknownFail, wantVerdict: CheckVerdictFail},
		{mode: onUnknownPass, wantVerdict: CheckVerdictPass},
		{mode: "", wantErr: true, wantVerdict: CheckVerdictFail},
		{mode: "ignore", wantErr: true, wantVerdict: CheckVerdictFail},
		{mode: "FAIL", wantErr: true, wantVerdict: CheckVerdictFail},
	}

	for _, tc := range tests {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			err := validateOnUnknown(tc.mode)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "--on-unknown")
			} else {
				require.NoError(t, err)
			}
			// An unrecognized mode still resolves to fail, so a validation gap
			// cannot turn into a silent pass.
			assert.Equal(t, tc.wantVerdict, unknownVerdict(tc.mode))
		})
	}
}

// TestAggregateVerdict_UnsetCheckVerdictIsNotAPass guards the extension point: a
// check appended later that forgets to set Verdict must not turn into a silent
// exit 0.
func TestAggregateVerdict_UnsetCheckVerdictIsNotAPass(t *testing.T) {
	checks := []Check{{Name: checkNameMinPassRate, Verdict: CheckVerdictPass}, {Name: "future_check"}}

	assert.Equal(t, CheckVerdictFail, aggregateVerdict(checks, onUnknownFail))
	assert.Equal(t, CheckVerdictPass, aggregateVerdict(checks, onUnknownPass))
}

func assertRate(t *testing.T, want, got *float64, what string) {
	t.Helper()
	if want == nil {
		assert.Nil(t, got, "%s must stay absent when nothing was measured", what)
		return
	}
	require.NotNil(t, got, what)
	assert.InDelta(t, *want, *got, 1e-9, what)
}
