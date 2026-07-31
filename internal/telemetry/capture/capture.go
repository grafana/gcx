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
// to it without risking an import cycle, and it holds no vocabulary and makes
// no privacy promise of its own: a slot stores whatever raw value its probe
// found, and for the Kubernetes reason that is server-supplied text. Mapping
// captured values to the wire contract (bucket labels, allowlists, the HTTP
// status range) belongs to the telemetry package next to Event — that
// filtering is the privacy guarantee, so nothing read from this package may
// reach the wire, a log, or any other output surface without passing it.
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

	// DryRun reports whether the operation ran in dry-run mode. False does not
	// imply anything was mutated — pull is read-only and always reports false —
	// so it only means something read together with the command.
	//
	// It is captured explicitly because it cannot be recovered from the recorded
	// flag names: --dry-run=false marks the flag as changed too, so the flag's
	// presence says nothing about its value, and two of the four commands set it
	// without having the flag at all.
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

// httpStatus is the HTTP transport status extracted from the invocation's
// surfaced error, written once at error-report time and read once at exit.
// Zero means no status was found. The 400–599 wire filter lives in the
// usage-event builder, not here.
//
//nolint:gochecknoglobals // process-wide invocation fact; see package doc.
var httpStatus atomic.Int64

// SetHTTPStatus records the HTTP transport status carried by the invocation's
// surfaced error. A zero status is a probe that found nothing and is ignored:
// a probe that found nothing must not erase a fact already recorded.
func SetHTTPStatus(status int) {
	if status == 0 {
		return
	}
	httpStatus.Store(int64(status))
}

// CurrentHTTPStatus returns the recorded HTTP status, or 0 when this
// invocation had none.
func CurrentHTTPStatus() int {
	return int(httpStatus.Load())
}

// k8sReason is the Kubernetes status reason extracted from the invocation's
// surfaced error, written once at error-report time and read once at exit.
// The value is the raw metav1.StatusReason string; the allowlist that keeps a
// server-supplied reason off the wire lives in the usage-event builder.
//
//nolint:gochecknoglobals // process-wide invocation fact; see package doc.
var k8sReason atomic.Pointer[string]

// SetK8sReason records the Kubernetes status reason carried by the
// invocation's surfaced error. An empty reason is a probe that found nothing
// (metav1.StatusReasonUnknown is the empty string) and is ignored.
func SetK8sReason(reason string) {
	if reason == "" {
		return
	}
	k8sReason.Store(&reason)
}

// CurrentK8sReason returns the recorded Kubernetes reason, or "" when this
// invocation had none.
func CurrentK8sReason() string {
	if reason := k8sReason.Load(); reason != nil {
		return *reason
	}
	return ""
}

// authMethodState is the recorded Grafana auth method plus the two facts a
// plain last-write value cannot carry: that two writers disagreed, and that
// the value came from the one caller whose answer outranks disagreement.
type authMethodState struct {
	value      string
	conflicted bool
	forced     bool
}

// grafanaAuthMethod is the authentication method the invocation selected for
// its Grafana connection, written by the config auth selector (and login) and
// read once at exit.
//
// Unlike the other slots this one is legitimately written concurrently:
// `gcx config check` resolves auth for every context at once, so the slot
// must stay coherent under parallel writers rather than assume the
// one-synchronous-write discipline the batch slot documents.
//
//nolint:gochecknoglobals // process-wide invocation fact; see package doc.
var grafanaAuthMethod atomic.Pointer[authMethodState]

// SetGrafanaAuthMethod records a decided Grafana auth method.
//
// An empty method is undecided and is ignored — a load that resolved no
// Grafana context must not erase a decision already made. "unknown" is a
// decided value, not an absence, and is recorded like any other.
//
// Two different decided values mean the invocation used more than one method
// (config check across contexts), and no single answer would be true; the
// slot collapses to a conflict and CurrentGrafanaAuthMethod reports nothing.
// The conflict is sticky: a later agreeing write cannot un-ask the question.
// A forced value is immune on the other side: Set never overwrites or
// conflicts it, so a config load racing login's authoritative answer — in
// either interleaving — cannot demote it.
func SetGrafanaAuthMethod(method string) {
	if method == "" {
		return
	}
	for {
		current := grafanaAuthMethod.Load()
		switch {
		case current == nil:
			if grafanaAuthMethod.CompareAndSwap(nil, &authMethodState{value: method}) {
				return
			}
		case current.forced || current.conflicted || current.value == method:
			return
		default:
			if grafanaAuthMethod.CompareAndSwap(current, &authMethodState{conflicted: true}) {
				return
			}
		}
	}
}

// ForceGrafanaAuthMethod records a decided Grafana auth method that outranks
// everything recorded before or after it by SetGrafanaAuthMethod, including a
// conflict. It exists for login, which resolves its method by probing: what
// login actually authenticated with is a better answer than anything a config
// load captured on the way, even when a mid-login retry switched methods. The
// forced state also wins races the plain store could lose — a Set retrying
// its CompareAndSwap against this write sees the forced value and yields
// instead of recording a conflict. A later Force replaces an earlier one
// (login's final attempt is the most final answer). An empty method is
// ignored.
func ForceGrafanaAuthMethod(method string) {
	if method == "" {
		return
	}
	grafanaAuthMethod.Store(&authMethodState{value: method, forced: true})
}

// CurrentGrafanaAuthMethod returns the decided Grafana auth method, or ""
// when none was decided or the deciders disagreed.
func CurrentGrafanaAuthMethod() string {
	if state := grafanaAuthMethod.Load(); state != nil && !state.conflicted {
		return state.value
	}
	return ""
}

// Reset clears every captured value.
//
// Production code never needs this — the process exits after one invocation.
// It exists so tests sharing a process cannot leak one case's captures into the
// next, which would otherwise make assertions pass or fail depending on test
// order.
func Reset() {
	batch.Store(nil)
	httpStatus.Store(0)
	k8sReason.Store(nil)
	grafanaAuthMethod.Store(nil)
}
