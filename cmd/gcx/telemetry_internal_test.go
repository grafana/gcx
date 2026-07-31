package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points the device-id and notice files at a temp dir so building an
// event never reads or writes the developer's real telemetry state, and clears
// any capture left behind by another case.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	capture.Reset()
	t.Cleanup(capture.Reset)
}

func pushInfo() *root.TelemetryInfo {
	return &root.TelemetryInfo{Command: "resources push", Flags: "path", OutputFormat: "text"}
}

// marshalEvent returns the event as it travels on the wire, so assertions can
// tell an empty field from an absent one.
func marshalEvent(t *testing.T, event telemetry.Event) map[string]any {
	t.Helper()
	data, err := json.Marshal(event)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	return fields
}

func TestBuildUsageEventOmitsBatchFieldsWithoutCapture(t *testing.T) {
	isolate(t)

	event := buildUsageEvent(&root.TelemetryInfo{Command: "resources get"}, time.Now(), 0)

	assert.Nil(t, event.BatchSucceededBucket)
	assert.Nil(t, event.BatchFailedBucket)
	assert.Nil(t, event.BatchSkippedBucket)
	assert.Nil(t, event.DryRun,
		"a command that captured no batch must report no volume at all")
}

func TestBuildUsageEventBucketsCapturedBatch(t *testing.T) {
	isolate(t)
	capture.SetBatch(capture.Batch{Succeeded: 47, Failed: 2, Skipped: 1, DryRun: false})

	event := buildUsageEvent(pushInfo(), time.Now(), 0)

	require.NotNil(t, event.BatchSucceededBucket)
	require.NotNil(t, event.DryRun)
	assert.Equal(t, telemetry.BucketToHundred, *event.BatchSucceededBucket)
	assert.Equal(t, telemetry.BucketTwoToFive, *event.BatchFailedBucket)
	assert.Equal(t, telemetry.BucketOne, *event.BatchSkippedBucket)
	assert.False(t, *event.DryRun)
}

// A batch that matched nothing is reported as bucket 0, not as an absent field:
// "matched nothing" and "was not a batch command" are different findings.
func TestBuildUsageEventReportsEmptyBatch(t *testing.T) {
	isolate(t)
	capture.SetBatch(capture.Batch{})

	event := buildUsageEvent(pushInfo(), time.Now(), 0)

	require.NotNil(t, event.BatchSucceededBucket)
	assert.Equal(t, telemetry.BucketZero, *event.BatchSucceededBucket)
	require.NotNil(t, event.DryRun)
	assert.False(t, *event.DryRun)
}

func TestBuildUsageEventCarriesDryRun(t *testing.T) {
	isolate(t)
	capture.SetBatch(capture.Batch{Succeeded: 3, DryRun: true})

	event := buildUsageEvent(pushInfo(), time.Now(), 0)

	require.NotNil(t, event.DryRun)
	assert.True(t, *event.DryRun, "a dry-run operation must be distinguishable from an applied one")
}

// Volume survives a partial failure, because the result document was written
// before the error was raised.
func TestBuildUsageEventKeepsBatchOnPartialFailure(t *testing.T) {
	isolate(t)
	capture.SetBatch(capture.Batch{Succeeded: 8, Failed: 2})

	event := buildUsageEvent(pushInfo(), time.Now(), 4)

	require.NotNil(t, event.BatchSucceededBucket)
	assert.Equal(t, telemetry.BucketSixToTwenty, *event.BatchSucceededBucket)
	assert.Equal(t, telemetry.OutcomeRuntimeError, event.Outcome)
	assert.Equal(t, "partial_failure", event.ErrorKind)
}

// The privacy contract for the batch fields, stated as what is actually true.
//
// An earlier version of this test was called NeverEmitsExactCounts, which is
// false: "0" and "1" are singleton categories, so those two sizes are exactly
// recoverable by design. What holds is narrower and is what governance was asked
// to approve — sizes leave the process only as one of seven fixed labels, and no
// raw numeric count field is sent at all.
//
// Asserts against decoded field values rather than a substring of the wire: the
// device ID is random hex, so a raw substring search for a small count would
// match it by chance and make this test flaky.
func TestBuildUsageEventSendsOnlyCategoryLabelsForBatchSizes(t *testing.T) {
	isolate(t)

	const succeeded, failed, skipped = 4312, 77, 913
	capture.SetBatch(capture.Batch{Succeeded: succeeded, Failed: failed, Skipped: skipped})

	data, err := json.Marshal(buildUsageEvent(pushInfo(), time.Now(), 0))
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))

	// Deliberately not a sweep comparing the fixture counts against every field:
	// duration_ms is a real number that could coincidentally equal one of them
	// and fail for a reason unrelated to the property. The assertions below are
	// what actually prove it — each batch field is a string from a seven-item
	// vocabulary, so no batch count, exact or otherwise, can be encoded there.

	declared := make(map[string]bool, len(telemetry.Buckets()))
	for _, label := range telemetry.Buckets() {
		declared[label] = true
	}
	for _, field := range []string{"batch_succeeded_bucket", "batch_failed_bucket", "batch_skipped_bucket"} {
		label, ok := fields[field].(string)
		require.True(t, ok, "%s must be a category label, not a number", field)
		assert.True(t, declared[label], "%s carries undeclared label %q", field, label)
	}

	assert.Equal(t, telemetry.BucketOverThousand, fields["batch_succeeded_bucket"])
	assert.Equal(t, telemetry.BucketToHundred, fields["batch_failed_bucket"])
	assert.Equal(t, telemetry.BucketToThousand, fields["batch_skipped_bucket"])
}

// 0 and 1 are singleton categories on purpose, so those sizes are exact. Pinned
// so nobody "fixes" the privacy story by folding them into a range without a
// governance decision, and so the honest wording in the docs stays honest.
func TestBuildUsageEventKeepsSingletonCategoriesExact(t *testing.T) {
	for count, want := range map[int]string{0: telemetry.BucketZero, 1: telemetry.BucketOne} {
		isolate(t)
		capture.SetBatch(capture.Batch{Succeeded: count})

		event := buildUsageEvent(pushInfo(), time.Now(), 0)

		require.NotNil(t, event.BatchSucceededBucket)
		assert.Equal(t, want, *event.BatchSucceededBucket,
			"a batch of %d must report its own singleton category", count)
	}
}

// Every emitted bucket value must come from the declared vocabulary, so the
// receiver never sees a label it does not know.
func TestBuildUsageEventEmitsOnlyDeclaredBuckets(t *testing.T) {
	declared := make(map[string]bool)
	for _, label := range telemetry.Buckets() {
		declared[label] = true
	}

	for _, n := range []int{0, 1, 3, 12, 60, 500, 5000} {
		isolate(t)
		capture.SetBatch(capture.Batch{Succeeded: n, Failed: n, Skipped: n})

		event := buildUsageEvent(pushInfo(), time.Now(), 0)

		for _, got := range []*string{
			event.BatchSucceededBucket, event.BatchFailedBucket, event.BatchSkippedBucket,
		} {
			require.NotNil(t, got)
			assert.True(t, declared[*got], "undeclared bucket label %q for count %d", *got, n)
		}
	}
}

// Every invocation whose final exit code is ExitCancelled reports the same
// outcome, whatever stopped it, and reports it as a stop rather than a failure:
// error_kind stays on the wire — it has no omitempty — but carries no kind.
func TestBuildUsageEventReportsCanceled(t *testing.T) {
	isolate(t)

	fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), gcxerrors.ExitCancelled))

	assert.Equal(t, telemetry.OutcomeCanceled, fields["outcome"])
	assert.InDelta(t, float64(gcxerrors.ExitCancelled), fields["exit_code"], 0)
	require.Contains(t, fields, "error_kind",
		"error_kind must stay serialized for a canceled invocation")
	assert.Empty(t, fields["error_kind"],
		"an invocation that stopped early has no error kind")
}

// The classification is on the final exit code, not on the route that produced
// it: a command that already wrote its own result document and returned an
// EmittedError carrying exit 5 reports the same outcome as the Ctrl-C path.
func TestBuildUsageEventReportsCanceledFromEmittedError(t *testing.T) {
	isolate(t)
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	err := fmt.Errorf("push: %w",
		gcxerrors.NewEmittedError(gcxerrors.ExitCancelled, context.Canceled))
	exitCode := reportError(err, nil, nil)
	require.Equal(t, gcxerrors.ExitCancelled, exitCode)

	event := buildUsageEvent(pushInfo(), time.Now(), exitCode)

	assert.Equal(t, telemetry.OutcomeCanceled, event.Outcome)
	assert.Empty(t, event.ErrorKind)
}

// Reporting cancellation must not disturb any other outcome, so the whole
// vocabulary is pinned against the final exit code.
func TestBuildUsageEventOutcomeVocabulary(t *testing.T) {
	for _, tc := range []struct {
		name          string
		help          bool
		exitCode      int
		wantOutcome   string
		wantErrorKind string
	}{
		{name: "success", exitCode: gcxerrors.ExitSuccess, wantOutcome: telemetry.OutcomeOK},
		{name: "help", help: true, exitCode: gcxerrors.ExitSuccess, wantOutcome: telemetry.OutcomeHelp},
		{name: "canceled", exitCode: gcxerrors.ExitCancelled, wantOutcome: telemetry.OutcomeCanceled},
		{name: "runtime error", exitCode: gcxerrors.ExitGeneralError, wantOutcome: telemetry.OutcomeRuntimeError, wantErrorKind: "error"},
		{name: "usage error", exitCode: gcxerrors.ExitUsageError, wantOutcome: telemetry.OutcomeRuntimeError, wantErrorKind: "usage_error"},
		{name: "auth failure", exitCode: gcxerrors.ExitAuthFailure, wantOutcome: telemetry.OutcomeRuntimeError, wantErrorKind: "auth_failure"},
		{name: "partial failure", exitCode: gcxerrors.ExitPartialFailure, wantOutcome: telemetry.OutcomeRuntimeError, wantErrorKind: "partial_failure"},
		{name: "version incompatible", exitCode: gcxerrors.ExitVersionIncompatible, wantOutcome: telemetry.OutcomeRuntimeError, wantErrorKind: "version_incompatible"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			info := pushInfo()
			info.Help = tc.help

			event := buildUsageEvent(info, time.Now(), tc.exitCode)

			assert.Equal(t, tc.wantOutcome, event.Outcome)
			assert.Equal(t, tc.wantErrorKind, event.ErrorKind)
		})
	}
}

// error_kind has no omitempty and must stay on the wire for every outcome;
// only the optional blocks — batch, parse, failure depth, auth method — may
// disappear.
func TestBuildUsageEventAlwaysEmitsErrorKind(t *testing.T) {
	for _, exitCode := range []int{0, 1, 2, 3, 4, 5, 6} {
		isolate(t)

		fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), exitCode))

		assert.Contains(t, fields, "error_kind",
			"error_kind must be present for exit code %d, even when empty", exitCode)
	}
}

// http_status is a transport failure status or nothing: only 400–599 reaches
// the wire, and everything else — no capture, a success status, a redirect,
// out-of-protocol values — is omitted rather than coerced.
func TestBuildUsageEventEmitsTransportHTTPStatusOnly(t *testing.T) {
	for status, want := range map[int]any{
		0:   nil,
		200: nil,
		302: nil,
		399: nil,
		400: float64(400),
		401: float64(401),
		403: float64(403),
		500: float64(500),
		599: float64(599),
		600: nil,
	} {
		isolate(t)
		capture.SetHTTPStatus(status)

		fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), 1))

		if want == nil {
			assert.NotContains(t, fields, "http_status",
				"captured status %d is not a transport failure and must be omitted", status)
		} else {
			assert.Equal(t, want, fields["http_status"], "captured status %d", status)
		}
	}
}

// k8s_reason reaches the wire only through the fixed vocabulary: every listed
// reason passes, anything else collapses to "other", and no capture means no
// field.
func TestBuildUsageEventClampsK8sReasonToAllowlist(t *testing.T) {
	for _, reason := range telemetry.K8sReasonLabels() {
		if reason == telemetry.K8sReasonOther {
			continue
		}
		isolate(t)
		capture.SetK8sReason(reason)

		event := buildUsageEvent(pushInfo(), time.Now(), 1)
		assert.Equal(t, reason, event.K8sReason, "listed reason must pass through unchanged")
	}

	isolate(t)
	capture.SetK8sReason("SomeFutureServerReason")
	event := buildUsageEvent(pushInfo(), time.Now(), 1)
	assert.Equal(t, telemetry.K8sReasonOther, event.K8sReason,
		"a server-controlled reason string must never travel verbatim")

	isolate(t)
	fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), 1))
	assert.NotContains(t, fields, "k8s_reason", "no captured reason means no field")
}

// grafana_auth_method reaches the wire only through the fixed vocabulary; an
// out-of-contract capture evidences a decision and travels as "unknown",
// never verbatim.
func TestBuildUsageEventClampsGrafanaAuthMethod(t *testing.T) {
	for _, method := range telemetry.GrafanaAuthMethodLabels() {
		isolate(t)
		capture.SetGrafanaAuthMethod(method)

		event := buildUsageEvent(pushInfo(), time.Now(), 0)
		assert.Equal(t, method, event.GrafanaAuthMethod, "listed method must pass through unchanged")
	}

	isolate(t)
	capture.SetGrafanaAuthMethod("Bearer secret-token-value")
	event := buildUsageEvent(pushInfo(), time.Now(), 0)
	assert.Equal(t, telemetry.AuthMethodUnknown, event.GrafanaAuthMethod,
		"an arbitrary captured string must be clamped, not forwarded")

	isolate(t)
	fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), 0))
	assert.NotContains(t, fields, "grafana_auth_method", "no decided method means no field")
}

// A partial failure has no single causal status: forty-seven resources may
// have failed forty-seven different ways, and the captured status belongs to
// whichever error happened to surface. Both failure-depth fields are
// suppressed — while the batch block, which is per-operation rather than
// per-error, survives exactly as TestBuildUsageEventKeepsBatchOnPartialFailure
// pins.
func TestBuildUsageEventSuppressesErrorSignalsOnPartialFailure(t *testing.T) {
	isolate(t)
	capture.SetBatch(capture.Batch{Succeeded: 8, Failed: 2})
	capture.SetHTTPStatus(500)
	capture.SetK8sReason("Conflict")

	fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), gcxerrors.ExitPartialFailure))

	assert.NotContains(t, fields, "http_status")
	assert.NotContains(t, fields, "k8s_reason")
	assert.Contains(t, fields, "batch_succeeded_bucket",
		"suppression is scoped to the failure-depth fields, never the batch block")
	assert.Equal(t, "partial_failure", fields["error_kind"])
}

// A canceled run is not a failure, so it reports no failure depth — whatever
// a probe captured before the interrupt landed. error_kind stays on the wire
// and empty, exactly as PR B pinned it.
func TestBuildUsageEventSuppressesErrorSignalsOnCanceled(t *testing.T) {
	isolate(t)
	capture.SetHTTPStatus(502)
	capture.SetK8sReason("Timeout")

	fields := marshalEvent(t, buildUsageEvent(pushInfo(), time.Now(), gcxerrors.ExitCancelled))

	assert.NotContains(t, fields, "http_status")
	assert.NotContains(t, fields, "k8s_reason")
	require.Contains(t, fields, "error_kind")
	assert.Empty(t, fields["error_kind"])
}

// The auth method describes the invocation, not the failure, so it survives
// every outcome — including the two exit codes that suppress the
// failure-depth fields.
func TestBuildUsageEventKeepsGrafanaAuthMethodOnEveryOutcome(t *testing.T) {
	for _, exitCode := range []int{
		gcxerrors.ExitSuccess, gcxerrors.ExitGeneralError, gcxerrors.ExitAuthFailure,
		gcxerrors.ExitPartialFailure, gcxerrors.ExitCancelled,
	} {
		isolate(t)
		capture.SetGrafanaAuthMethod("token")

		event := buildUsageEvent(pushInfo(), time.Now(), exitCode)

		assert.Equal(t, "token", event.GrafanaAuthMethod,
			"auth method must survive exit code %d", exitCode)
	}
}

// Path safety is deliberately not asserted here. buildUsageEvent copies
// TelemetryInfo.Command, .Flags and .OutputFormat verbatim — it has no
// sanitisation of its own, so a test feeding it a path would be asserting a
// guarantee this layer does not provide, and one feeding it only flag names
// would pass for any implementation.
//
// The filtering lives in cmd/gcx/root: changedFlagNames records flag names only,
// and resolvedOutputFormat allowlists the format so that commands where
// --output is a directory cannot leak it. Both are tested there, in
// root/telemetry_internal_test.go.
//
// What this layer owns is the batch block, the failure-depth fields and the
// auth method, and the tests above cover them: bucket category labels only,
// the 400–599 transport filter, the k8s reason allowlist, the auth-method
// clamp, and the exit-4/5 suppression scope.
