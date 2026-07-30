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

// Batch describes a batch resource operation that ran to a finalized count.
//
// Presence is meaningful: a nil *Batch means this invocation was not a batch
// resource operation, or it aborted before its counts were final. That is why
// the counts travel together in one struct rather than as four independent
// globals — "all present or all absent" is then an invariant of the type, not a
// rule each caller has to remember.
type Batch struct {
	// Succeeded, Failed and Skipped are the operation's finalized counts.
	//
	// Units differ per command and must not be compared or totalled across
	// them. Pull is the case that makes this concrete, and its Failed count is
	// genuinely mixed: the puller records one failure per resource whose file
	// write fails, but also one failure per whole resource *type* when a list
	// call fails, and one skip per type the server cannot list at all. So a
	// pull failure count of 2 can mean two dashboards or two entire types.
	//
	// These are also not a complete partition of the work. A resource filtered
	// out before processing (an unmanaged resource without --include-managed,
	// a resource type the delete API does not support) is recorded in none of
	// the three, so all-zero counts do not prove nothing matched.
	Succeeded int
	Failed    int
	Skipped   int

	// DryRun reports whether the operation was a rehearsal. It is captured
	// explicitly because it cannot be recovered from the recorded flag names:
	// --dry-run=false marks the flag as changed too, so the flag's presence
	// says nothing about its value.
	DryRun bool
}

// batch is written by the resource commands once their operation has finished
// and its counts are final, and read once at exit.
//
//nolint:gochecknoglobals // process-wide invocation fact; see package doc.
var batch atomic.Pointer[Batch]

// SetBatch records the outcome of a batch resource operation.
//
// Callers must only call this once the operation has completed and its counts
// are final — not before, so a hard abort reports nothing, and not conditional
// on output succeeding, because work already performed is not undone by a
// failure to render or write the summary.
//
// The last call wins. A command that runs more than one operation reports the
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
