package capture_test

import (
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNothingCapturedByDefault(t *testing.T) {
	capture.Reset()

	assert.Nil(t, capture.CurrentBatch(),
		"an invocation that ran no batch operation must report no volume")
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

// The value is written from inside errgroup-driven batch pipelines, so the
// accessors must be safe to call concurrently. Run with -race.
func TestConcurrentAccessIsSafe(t *testing.T) {
	capture.Reset()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			capture.SetBatch(capture.Batch{Succeeded: i})
		}()
		go func() {
			defer wg.Done()
			_ = capture.CurrentBatch()
		}()
	}
	wg.Wait()

	assert.NotNil(t, capture.CurrentBatch(), "some write must have landed")
}

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
