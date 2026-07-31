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
	if capture.CurrentHTTPStatus() != 0 {
		fmt.Fprintln(os.Stderr,
			"capture package has a non-zero http status default: every invocation would report a failure status")
		os.Exit(1)
	}
	if capture.CurrentK8sReason() != "" {
		fmt.Fprintln(os.Stderr,
			"capture package has a non-empty k8s reason default: every invocation would report a Kubernetes failure")
		os.Exit(1)
	}
	if capture.CurrentGrafanaAuthMethod() != "" {
		fmt.Fprintln(os.Stderr,
			"capture package has a non-empty auth method default: invocations that never touched Grafana would report one")
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
	capture.SetHTTPStatus(503)
	capture.SetK8sReason("NotFound")
	capture.SetGrafanaAuthMethod("token")
	require.NotNil(t, capture.CurrentBatch())
	require.Equal(t, 503, capture.CurrentHTTPStatus())
	require.Equal(t, "NotFound", capture.CurrentK8sReason())
	require.Equal(t, "token", capture.CurrentGrafanaAuthMethod())

	capture.Reset()

	assert.Nil(t, capture.CurrentBatch())
	assert.Zero(t, capture.CurrentHTTPStatus())
	assert.Empty(t, capture.CurrentK8sReason())
	assert.Empty(t, capture.CurrentGrafanaAuthMethod())
}

func TestSetHTTPStatusRoundTripsAndIgnoresZero(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	capture.SetHTTPStatus(403)
	assert.Equal(t, 403, capture.CurrentHTTPStatus())

	capture.SetHTTPStatus(0)
	assert.Equal(t, 403, capture.CurrentHTTPStatus(),
		"a probe that found nothing must not erase a fact already recorded")

	capture.SetHTTPStatus(500)
	assert.Equal(t, 500, capture.CurrentHTTPStatus(), "the last found status wins")
}

func TestSetK8sReasonRoundTripsAndIgnoresEmpty(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	capture.SetK8sReason("Forbidden")
	assert.Equal(t, "Forbidden", capture.CurrentK8sReason())

	capture.SetK8sReason("")
	assert.Equal(t, "Forbidden", capture.CurrentK8sReason(),
		"StatusReasonUnknown is the empty string and means nothing was found")
}

func TestSetGrafanaAuthMethodUndecidedNeverErasesDecided(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	capture.SetGrafanaAuthMethod("oauth")
	capture.SetGrafanaAuthMethod("")
	assert.Equal(t, "oauth", capture.CurrentGrafanaAuthMethod())

	// "unknown" is a decided value, not an absence: it means a selection ran
	// and could not classify the method, which is a different fact from no
	// selection having run. It participates in overwrite and conflict rules
	// like any other decided value.
	capture.Reset()
	capture.SetGrafanaAuthMethod("unknown")
	assert.Equal(t, "unknown", capture.CurrentGrafanaAuthMethod())
}

func TestSetGrafanaAuthMethodConflictReportsNothing(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	capture.SetGrafanaAuthMethod("token")
	capture.SetGrafanaAuthMethod("token")
	assert.Equal(t, "token", capture.CurrentGrafanaAuthMethod(),
		"an agreeing repeat write is not a conflict")

	capture.SetGrafanaAuthMethod("oauth")
	assert.Empty(t, capture.CurrentGrafanaAuthMethod(),
		"two different decided methods have no single true answer")

	capture.SetGrafanaAuthMethod("oauth")
	assert.Empty(t, capture.CurrentGrafanaAuthMethod(),
		"a conflict is sticky: a later agreeing write cannot un-ask the question")
}

func TestForceGrafanaAuthMethodOutranksConflict(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	capture.SetGrafanaAuthMethod("token")
	capture.SetGrafanaAuthMethod("oauth")
	require.Empty(t, capture.CurrentGrafanaAuthMethod())

	capture.ForceGrafanaAuthMethod("mtls")
	assert.Equal(t, "mtls", capture.CurrentGrafanaAuthMethod(),
		"login's probe-resolved answer outranks anything captured on the way")

	capture.ForceGrafanaAuthMethod("")
	assert.Equal(t, "mtls", capture.CurrentGrafanaAuthMethod(), "an empty force is a no-op")
}

// Every real batch caller writes once, synchronously, after its operation has
// finished — never from a worker goroutine. That is a property of the call
// sites, not of this package, and it matters: a write from inside an errgroup
// worker would report one arbitrary worker's partial counts instead of the
// finalized summary. The call sites are pinned in
// cmd/gcx/resources/batchvolume_test.go; there is deliberately no concurrency
// test here, because exercising atomic.Pointer under -race would only restate
// its own contract and would read as sanctioning concurrent writes.
//
// The Grafana auth method slot is the documented exception: `gcx config check`
// resolves auth for every context concurrently, so parallel writers are a real
// production pattern there. The -race test for that pattern lives next to the
// caller that owns it, in internal/config, not here — the property under test
// is the caller's, and the slot's conflict rule is what makes the outcome
// deterministic regardless of write order.

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
