// Package capture holds process-wide facts about the current gcx invocation
// that the usage-stats emitter reads once, at exit.
//
// Why a package of globals: a command's result never reaches main. Commands
// return only an error, so a successful push has nothing to hand back, and
// threading a telemetry argument through every pipeline would touch the whole
// call graph to serve one consumer. The established alternative in this repo is
// a process-global written mid-run and read at exit — see
// config.CapturedTargetKind, which does exactly this for target_kind. This
// package is that pattern given one home, so the next signal does not add a
// fourth bespoke global to a fourth unrelated package.
//
// This package deliberately imports nothing from gcx, so any package can write
// to it without risking an import cycle, and it holds no vocabulary: values are
// raw counts and booleans. Mapping them to the wire contract (bucket labels,
// allowlists) belongs to the telemetry package next to Event, so the privacy
// filtering all lives in one place.
//
// Every value here is read by cmd/gcx's usage-event builder and must obey the
// same privacy invariant as telemetry.Event: shape only, never content.
package capture

import "sync/atomic"

// Batch describes a completed batch resource operation, as the user saw it.
//
// Presence is meaningful: a nil *Batch means this invocation was not a batch
// resource operation, or it aborted before emitting a result. That is why the
// counts travel together in one struct rather than as four independent globals
// — "all present or all absent" is then an invariant of the type, not a rule
// each caller has to remember.
type Batch struct {
	// Succeeded, Failed and Skipped mirror the finalized summary the command
	// printed. Units differ per command and must not be compared across them:
	// on the pull path successes count resource instances while failures and
	// skips count filters, because the puller iterates filters and records one
	// failure per filter.
	Succeeded int
	Failed    int
	Skipped   int

	// DryRun reports whether the operation was a rehearsal. It is captured
	// explicitly because it cannot be recovered from the recorded flag names:
	// --dry-run=false marks the flag as changed too, so the flag's presence
	// says nothing about its value.
	DryRun bool
}

// batch is written by the resource commands after their result document is
// emitted, and read once at exit.
//
//nolint:gochecknoglobals // process-wide invocation fact; see package doc.
var batch atomic.Pointer[Batch]

// SetBatch records the outcome of a batch resource operation.
//
// Callers must only call this once the command's final result document or
// receipt has been written successfully. That timing is the whole contract:
// what gets reported is exactly what the user was shown, so a run that aborted
// before printing a summary reports no volume rather than an internal count
// nobody saw.
//
// The last call wins. A command that emits more than one result reports the
// most recent, which is the closest thing to a final answer available at exit.
func SetBatch(b Batch) {
	batch.Store(&b)
}

// CurrentBatch returns the recorded batch outcome, or nil when this invocation
// had none.
func CurrentBatch() *Batch {
	return batch.Load()
}

// Reset clears every captured value.
//
// Production code never needs this — the process exits after one invocation.
// It exists so tests sharing a process cannot leak one case's captures into the
// next, which would otherwise make assertions pass or fail depending on test
// order.
func Reset() {
	batch.Store(nil)
}
