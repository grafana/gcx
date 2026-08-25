package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points the device-id and notice files at a temp dir so building an
// event never reads or writes the developer's real telemetry state, and clears
// any capture left behind by another case.
//
// Deliberately hand-rolled rather than calling testutils.SandboxConfigEnv,
// which covers strictly more (HOME, the config dirs, agent mode). This package
// cannot import internal/testutils at all: its package init() unsets
// GCX_AGENT_MODE process-wide, and the subprocess tests in main_test.go re-exec
// this very test binary with GCX_AGENT_MODE=true to assert agent-mode output.
// The child's init would wipe the variable before the helper runs, so importing
// testutils anywhere in this package turns those tests red with human-formatted
// output. Verified: the swap fails TestConfigCheckProcessExit and
// TestConfigSetPlaintextFallbackWarningProcess.
//
// buildUsageEvent reads a second process-global besides this package's, so
// target_kind is cleared here too: it is set by whichever case last loaded a
// config and would otherwise bleed into unrelated cases, which is the exact
// cross-case leak this helper exists to stop.
//
// The gap that remains is real but currently unasserted: buildUsageEvent also
// derives IsAgent and Agent from agent.IsAgentMode(), IsCI and CIProvider from
// telemetry.DetectCI(), and IsTTY from the real stdout. No case here asserts on
// any of those. A case that does must pin them locally — it cannot reach for
// the shared sandbox.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	capture.Reset()
	t.Cleanup(capture.Reset)
	internalconfig.ClearCapturedTargetKind()
	t.Cleanup(internalconfig.ClearCapturedTargetKind)
}

func pushInfo() *root.TelemetryInfo {
	return &root.TelemetryInfo{Command: "resources push", Flags: "path", OutputFormat: "text"}
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

// error_kind has no omitempty and must stay on the wire for every outcome;
// only the new batch fields are allowed to disappear.
func TestBuildUsageEventAlwaysEmitsErrorKind(t *testing.T) {
	for _, exitCode := range []int{0, 1, 2, 4} {
		isolate(t)

		data, err := json.Marshal(buildUsageEvent(pushInfo(), time.Now(), exitCode))
		require.NoError(t, err)

		var fields map[string]any
		require.NoError(t, json.Unmarshal(data, &fields))
		assert.Contains(t, fields, "error_kind",
			"error_kind must be present for exit code %d, even when empty", exitCode)
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
// What this layer owns is the batch block, and the tests above cover it: bucket
// category labels only, from the declared vocabulary, and no raw batch count.
