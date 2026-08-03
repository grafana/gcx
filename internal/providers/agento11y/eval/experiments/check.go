package experiments

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// Experiment lifecycle statuses used by the eval API. completed, failed and
// canceled are the finished ones; any other value means the experiment is still
// producing results, or the server sent a status this build does not know.
const (
	statusCompleted = "completed"
	statusFailed    = "failed"
	statusCanceled  = "canceled"
)

// Check names are stable identifiers for CI consumers, so they are part of the
// output contract.
const (
	checkNameMinPassRate      = "min_pass_rate"
	checkNameVerdictCoverage  = "verdict_coverage"
	checkNameExperimentStatus = "experiment_status"
)

// Discriminators for the check document. A failing check writes this document
// in place of a gcx.error, so the marker has to be on this shape too, otherwise
// a CI consumer has to guess which of the two it received.
const (
	checkResultType          = "gcx.agento11y.experiment_check"
	checkResultSchemaVersion = "1"
)

// Values of --on-unknown.
const (
	onUnknownFail = "fail"
	onUnknownPass = "pass"
)

// CheckVerdict is the outcome of one Check, and of the CheckResult that
// aggregates them.
type CheckVerdict string

const (
	CheckVerdictPass    CheckVerdict = "pass"
	CheckVerdictFail    CheckVerdict = "fail"
	CheckVerdictUnknown CheckVerdict = "unknown"
)

// CheckResult is the document the check command writes to stdout, on both pass
// and failure. Checks is a slice rather than one boolean so further thresholds
// are additive.
//
// TestCaseCount, PassCount and PassDenominator carry no omitempty, for the
// reason ExperimentReportSummary documents.
type CheckResult struct {
	Type             string       `json:"type"`
	SchemaVersion    string       `json:"schema_version"`
	ExperimentID     string       `json:"experiment_id"`
	ExperimentStatus string       `json:"experiment_status"`
	Verdict          CheckVerdict `json:"verdict"`
	TestCaseCount    int          `json:"test_case_count"`
	PassCount        int          `json:"pass_count"`
	PassDenominator  int          `json:"pass_denominator"`
	Checks           []Check      `json:"checks"`
}

// Check is one threshold or state assertion within a CheckResult. Threshold and
// Actual are optional: a status check has no numbers, and a check that could not
// measure anything has no actual value.
type Check struct {
	Name      string       `json:"name"`
	Verdict   CheckVerdict `json:"verdict"`
	Threshold *float64     `json:"threshold,omitempty"`
	Actual    *float64     `json:"actual,omitempty"`
	Observed  string       `json:"observed"`
}

// checkSpec is the resolved set of thresholds a check applies.
type checkSpec struct {
	// MinPassRate is the lowest acceptable experiment pass rate, 0 through 1.
	// Nil means the caller did not pass --min-pass-rate, which skips the check.
	MinPassRate *float64
	// MinVerdictCoverage is the lowest acceptable share of test cases that
	// produced a pass or fail verdict, 0 through 1. Zero disables the check.
	MinVerdictCoverage float64
	// OnUnknown is the --on-unknown value, which decides the overall verdict
	// when a check cannot measure what it grades.
	OnUnknown string
}

// newCheckResult starts a result for one experiment, with the discriminators and
// the server's counts filled in and no checks yet.
func newCheckResult(run Experiment, summary ExperimentReportSummary) CheckResult {
	return CheckResult{
		Type:             checkResultType,
		SchemaVersion:    checkResultSchemaVersion,
		ExperimentID:     run.ID(),
		ExperimentStatus: run.Status,
		TestCaseCount:    summary.TestCaseCount,
		PassCount:        summary.PassCount,
		PassDenominator:  summary.PassDenominator,
		Checks:           []Check{},
	}
}

func normalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

// isFinishedStatus reports whether the experiment reached a terminal status.
// An unrecognized status counts as unfinished: the command refuses to grade a
// run it does not understand rather than guess.
func isFinishedStatus(status string) bool {
	switch normalizeStatus(status) {
	case statusCompleted, statusFailed, statusCanceled:
		return true
	}
	return false
}

// validateOnUnknown rejects an --on-unknown value the command does not know. The
// caller turns the error into a usage error, the way resolveMetricsMode is used
// in internal/providers/appo11y/services.
func validateOnUnknown(mode string) error {
	switch mode {
	case onUnknownFail, onUnknownPass:
		return nil
	default:
		return fmt.Errorf("invalid --on-unknown value %q: must be %q or %q", mode, onUnknownFail, onUnknownPass)
	}
}

// unknownVerdict maps --on-unknown onto the verdict an unmeasurable check
// contributes. Any value other than pass, including the empty string, resolves
// to failure, so a run this command could not grade never passes by accident.
func unknownVerdict(mode string) CheckVerdict {
	if mode == onUnknownPass {
		return CheckVerdictPass
	}
	return CheckVerdictFail
}

// checkStatusError says why an unfinished experiment cannot be graded, and
// returns nil when the experiment can be graded. It exits 1 rather than the 4 a
// quality verdict carries, so a CI job can tell a regression from an experiment
// that is still running.
func checkStatusError(runID, status string) error {
	if isFinishedStatus(status) {
		return nil
	}

	reported := strings.TrimSpace(status)
	if reported == "" {
		reported = "(none)"
	}
	return &gcxerrors.DetailedError{
		Summary: "Experiment has not finished",
		Details: fmt.Sprintf("experiment %s reports status %q, and check grades only a finished experiment", runID, reported),
		Suggestions: []string{
			fmt.Sprintf("Watch the run with 'gcx agento11y experiments get %s' and retry once the status is %s", runID, statusCompleted),
		},
	}
}

// evaluateChecks compares an experiment report against the thresholds in spec
// and returns the document the check command writes to stdout. It is pure: no
// I/O, no clock, no network.
//
// The caller rejects an unfinished experiment first, because being unable to
// grade a running experiment is a state error and not a quality verdict. See
// checkStatusError.
//
// Thresholds are compared with strict less-than, so --min-pass-rate 0.9 accepts
// exactly 0.9.
func evaluateChecks(report *ExperimentReport, spec checkSpec) CheckResult {
	if report == nil {
		report = &ExperimentReport{}
	}
	run := report.Experiment
	summary := report.Summary
	result := newCheckResult(run, summary)

	// The status check appears in every result, so the document names at least
	// one assertion even when both thresholds are disabled.
	status := statusCheck(run.Status)
	result.Checks = append(result.Checks, status)

	// A run that died halfway must not pass because the trials that did execute
	// happened to clear the threshold.
	//
	// This branch grades only failed and canceled runs when the command calls it,
	// because the RunE of check rejects every other unfinished status through
	// checkStatusError first. Dropping that call would route an unfinished run
	// here and turn its exit 1 into an exit 4.
	if status.Verdict != CheckVerdictPass {
		result.Verdict = CheckVerdictFail
		return result
	}

	if spec.MinPassRate != nil {
		result.Checks = append(result.Checks, evaluateMinPassRate(summary, *spec.MinPassRate))
	}
	if spec.MinVerdictCoverage > 0 {
		result.Checks = append(result.Checks, evaluateVerdictCoverage(summary, spec.MinVerdictCoverage))
	}
	result.Verdict = aggregateVerdict(result.Checks, spec.OnUnknown)
	return result
}

// statusCheck is the experiment_status assertion.
func statusCheck(status string) Check {
	check := Check{Name: checkNameExperimentStatus}
	if normalizeStatus(status) == statusCompleted {
		check.Verdict = CheckVerdictPass
		check.Observed = fmt.Sprintf("experiment status is %q", status)
		return check
	}
	check.Verdict = CheckVerdictFail
	check.Observed = fmt.Sprintf("experiment status is %q, not %q", status, statusCompleted)
	return check
}

// evaluateMinPassRate builds the min_pass_rate check. A nil pass rate is the
// server saying no test case produced a pass or fail verdict, which is not a
// measured 0%.
func evaluateMinPassRate(summary ExperimentReportSummary, threshold float64) Check {
	check := Check{Name: checkNameMinPassRate, Threshold: &threshold}

	if summary.PassRate == nil {
		check.Verdict = CheckVerdictUnknown
		check.Observed = "no test case produced a pass or fail verdict"
		return check
	}

	actual := *summary.PassRate
	check.Actual = &actual
	// A server older than the pass-count fields sends a rate with no counts. The
	// rate is still authoritative, so grade it and say the counts are missing
	// instead of inventing a 0/0.
	if summary.PassDenominator > 0 {
		check.Observed = fmt.Sprintf("%d of %d graded test cases passed on the first completed attempt (%s)",
			summary.PassCount, summary.PassDenominator, formatRate(actual))
	} else {
		check.Observed = fmt.Sprintf("pass rate %s, and the server reported no pass counts", formatRate(actual))
	}
	check.Verdict = verdictForThreshold(actual, threshold)
	return check
}

// evaluateVerdictCoverage builds the verdict_coverage check, which guards the
// blind spot in pass_rate: a test case whose trials all failed produces no
// verdict, so it leaves both sides of that fraction untouched. A run that lost
// most of its suite can still report a perfect rate for the few test cases that
// survived.
//
// A check that cannot measure coverage reports unknown instead of dropping out
// of the result, so --on-unknown decides it and the CI log names the gate that
// did not run.
func evaluateVerdictCoverage(summary ExperimentReportSummary, threshold float64) Check {
	check := Check{Name: checkNameVerdictCoverage, Threshold: &threshold}
	lost := summary.FailedCount + summary.CanceledCount

	switch {
	case summary.TestCaseCount <= 0:
		check.Verdict = CheckVerdictUnknown
		check.Observed = "the server reported no test case count, so the share of graded test cases is unknown"
		return check
	case summary.PassDenominator == 0 && summary.CompletedCount > 0:
		// Trials completed and still produced no verdict, so the evaluators emit
		// only reward or numeric scores. Coverage of a verdict that no evaluator
		// produces measures nothing.
		check.Verdict = CheckVerdictUnknown
		check.Observed = fmt.Sprintf("%d trials completed and none produced a pass or fail verdict, so coverage measures nothing", summary.CompletedCount)
		return check
	}

	// A server that counts more verdicts than test cases pushes this above 1. The
	// share is not clamped: the comparison against the threshold stays correct,
	// and the odd percentage in the output is the evidence that the counts
	// disagree.
	actual := float64(summary.PassDenominator) / float64(summary.TestCaseCount)
	check.Actual = &actual
	check.Observed = fmt.Sprintf("%d of %d test cases produced a verdict (%s)",
		summary.PassDenominator, summary.TestCaseCount, formatRate(actual))
	if lost > 0 {
		check.Observed += fmt.Sprintf("; %d trials failed or were canceled", lost)
	}
	check.Verdict = verdictForThreshold(actual, threshold)
	return check
}

func verdictForThreshold(actual, threshold float64) CheckVerdict {
	if actual < threshold {
		return CheckVerdictFail
	}
	return CheckVerdictPass
}

// aggregateVerdict folds per-check outcomes into the verdict of the result. Any
// failing check fails the result; otherwise an unknown check decides the outcome
// through onUnknown. A check that set no verdict at all is treated as unknown,
// so a future check that forgets to set one cannot turn into a silent pass.
func aggregateVerdict(checks []Check, onUnknown string) CheckVerdict {
	verdict := CheckVerdictPass
	for _, c := range checks {
		switch c.Verdict {
		case CheckVerdictFail:
			return CheckVerdictFail
		case CheckVerdictPass:
		default:
			verdict = unknownVerdict(onUnknown)
		}
	}
	return verdict
}

// checkFailureSummary renders the one-line stderr diagnostic for a non-passing
// result. EmittedError suppresses the top-level reporter's stderr rendering, so
// without this line a failing check prints nothing a human can read.
func checkFailureSummary(result CheckResult) string {
	id := result.ExperimentID
	if id == "" {
		id = "(unknown)"
	}

	details := make([]string, 0, len(result.Checks))
	for _, c := range result.Checks {
		if c.Verdict == CheckVerdictPass {
			continue
		}
		details = append(details, checkDetail(c))
	}
	// Every non-passing result names the check that produced it, so this stands in
	// only for a result built without one. Without it the line would end in a
	// colon and say nothing.
	if len(details) == 0 {
		details = append(details, "no check reported a reason")
	}
	return fmt.Sprintf("quality check verdict %s for experiment %s: %s",
		result.Verdict, id, strings.Join(details, "; "))
}

// checkDetail renders one check as a single line. Both stdout (the text codec)
// and stderr (the failure diagnostic) use it, so a CI log shows one wording for
// one check.
func checkDetail(c Check) string {
	detail := fmt.Sprintf("%s %s", c.Name, c.Verdict)
	if c.Threshold != nil {
		detail += fmt.Sprintf(" (threshold %s)", formatRate(*c.Threshold))
	}
	if c.Observed != "" {
		detail += " - " + c.Observed
	}
	return detail
}

func formatRate(v float64) string {
	return fmt.Sprintf("%.2f%%", v*100)
}
