package capture_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain observes the package default before any test has written to it,
// which is the only point where "nothing captured unless something captured it"
// can actually be checked. A test that calls Reset() first and then asserts nil
// would pass even if a future init gave the global a non-nil default — and that
// default would make every non-batch invocation (gcx login, gcx resources get,
// a parse error) emit batch fields, destroying the meaning of their absence.
func TestMain(m *testing.M) {
	if capture.CurrentBatch() != nil {
		fmt.Fprintln(os.Stderr,
			"capture package has a non-nil default: every non-batch invocation would report volume")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestSetBatchRoundTrips(t *testing.T) {
	capture.Reset()

	capture.SetBatch(capture.Batch{Succeeded: 47, Failed: 2, Skipped: 1, DryRun: true})

	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, capture.Batch{Succeeded: 47, Failed: 2, Skipped: 1, DryRun: true}, *got)
}

// A zero-count batch is a real outcome — a run that matched nothing — and must
// be distinguishable from no batch at all.
func TestZeroBatchIsStillPresent(t *testing.T) {
	capture.Reset()

	capture.SetBatch(capture.Batch{})

	got := capture.CurrentBatch()
	require.NotNil(t, got, "a batch that matched nothing must still be reported")
	assert.Equal(t, capture.Batch{}, *got)
}

func TestLastWriteWins(t *testing.T) {
	capture.Reset()

	capture.SetBatch(capture.Batch{Succeeded: 1})
	capture.SetBatch(capture.Batch{Succeeded: 9, Failed: 3})

	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, 9, got.Succeeded, "the most recent result is the closest to final")
	assert.Equal(t, 3, got.Failed)
}

// Reset is what keeps in-process tests independent. Without it, one test's
// capture would leak into the next and assertions would pass or fail depending
// on test order.
func TestResetClearsEverything(t *testing.T) {
	capture.SetBatch(capture.Batch{Succeeded: 5})
	require.NotNil(t, capture.CurrentBatch())

	capture.Reset()

	assert.Nil(t, capture.CurrentBatch())
}

// Every real caller writes once, synchronously, after its operation has
// finished — never from a worker goroutine. That is a property of the call
// sites, not of this package, and it matters: a write from inside an errgroup
// worker would report one arbitrary worker's partial counts instead of the
// finalized summary. The call sites are pinned in
// cmd/gcx/resources/batchvolume_test.go; there is deliberately no concurrency
// test here, because exercising atomic.Pointer under -race would only restate
// its own contract and would read as sanctioning concurrent writes.

// Batch is copied in and out, so a caller mutating its own struct afterwards
// cannot retroactively change what telemetry reports.
func TestCapturedValueIsIndependentOfCaller(t *testing.T) {
	capture.Reset()

	b := capture.Batch{Succeeded: 10}
	capture.SetBatch(b)
	b.Succeeded = 999

	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, 10, got.Succeeded, "later caller mutation must not affect the capture")
}
