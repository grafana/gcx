package main

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
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
	assert.True(t, *event.DryRun, "a rehearsal must be distinguishable from an applied run")
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

	// No field carries a raw count, in either encoding. This is the claim that
	// survives the singleton categories: a large batch is never recoverable.
	for name, value := range fields {
		for _, count := range []int{succeeded, failed, skipped} {
			assert.NotEqual(t, float64(count), value,
				"field %q carries raw count %d; categories only", name, count)
			assert.NotEqual(t, strconv.Itoa(count), value,
				"field %q carries raw count %d as a string; categories only", name, count)
		}
	}

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
// labels only, from the declared vocabulary, with no exact count on the wire.
