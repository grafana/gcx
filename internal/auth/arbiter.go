package auth

import (
	"fmt"
	"sync"
	"time"
)

// defaultCallbackAcquireTimeout bounds how long a flow waits for a callback
// that belongs to it. Without a bound the only limit is context cancellation,
// so a login that never receives a valid callback waits forever.
//
// This exists to stop an unattended hang, not to hurry the user along, so it is
// set well past any interactive round trip. The clock starts when the listener
// binds and covers the whole consent flow — SSO, MFA, and, in the remote case,
// copying a URL between two computers — and the authorization code is minted at
// the *end* of that, so a bound tight enough to be a nuisance would reject
// perfectly good logins.
const defaultCallbackAcquireTimeout = 30 * time.Minute

// callbackClaim reports whether a completion path may run the token exchange.
type callbackClaim int

const (
	// claimGranted means the caller owns the flow and must run the exchange.
	claimGranted callbackClaim = iota
	// claimTaken means another path owns the flow, or already finished it.
	claimTaken
	// claimExpired means the acquisition deadline passed first.
	claimExpired
)

type arbiterState int

const (
	arbiterIdle arbiterState = iota
	arbiterExchanging
	arbiterDone
	arbiterExpired
)

// callbackArbiter is the single ownership authority for one OAuth flow.
//
// Three paths race to complete a flow: the HTTP callback handler (on the
// server's goroutine), a pasted redirect URL (on the flow's own goroutine), and
// the acquisition deadline. All three decide ownership here, so at most one
// token exchange is ever in flight and exactly one of them ends the flow.
//
// It replaces the sync.Once that used to guard the callback handler. That guard
// was claimed on arrival, before the state check, so any request at all — from
// any local process, or a stale tab from the previous OAuth leg — consumed it
// and aborted the login. Ownership belongs to the state check, not to whoever
// knocks first.
type callbackArbiter struct {
	mu     sync.Mutex
	state  arbiterState
	timer  *time.Timer
	expiry chan struct{}
}

// newCallbackArbiter starts the acquisition deadline. The deadline is absolute:
// it is set once and never extended, so it bounds the whole flow rather than
// the gap between attempts.
func newCallbackArbiter(timeout time.Duration) *callbackArbiter {
	a := &callbackArbiter{expiry: make(chan struct{})}
	a.timer = time.AfterFunc(timeout, a.deadlineReached)
	return a
}

// claim asks for the right to run the token exchange. Only a caller that has
// already established the callback belongs to this flow may call it: claim
// decides concurrency, not ownership.
func (a *callbackArbiter) claim() callbackClaim {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch a.state {
	case arbiterIdle:
		a.state = arbiterExchanging
		return claimGranted
	case arbiterExpired:
		return claimExpired
	case arbiterExchanging, arbiterDone:
		return claimTaken
	}
	return claimTaken
}

// settle marks a claimed flow finished. The claim is permanent from here, so a
// replayed callback is rejected rather than exchanged a second time.
func (a *callbackArbiter) settle() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == arbiterExchanging {
		a.state = arbiterDone
	}
}

// expired closes once the acquisition deadline has passed with no claim holding
// the flow. The flow selects on it alongside the result and error channels.
func (a *callbackArbiter) expired() <-chan struct{} { return a.expiry }

// stop releases the deadline timer. The flow defers it.
func (a *callbackArbiter) stop() { a.timer.Stop() }

// deadlineReached runs on the timer goroutine. It takes the same lock as claim,
// so a callback arriving at the deadline either wins the flow outright or is
// told the flow expired — it can never half-claim one that is already expiring.
func (a *callbackArbiter) deadlineReached() {
	a.mu.Lock()
	switch a.state {
	case arbiterIdle:
		a.state = arbiterExpired
	case arbiterExchanging:
		// A claim is mid-exchange. Cutting it off could throw away a token the
		// server has already issued, and the exchange carries its own timeout,
		// so let it finish; it always delivers a result or an error.
		a.mu.Unlock()
		return
	case arbiterDone, arbiterExpired:
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	close(a.expiry)
}

// errCallbackTimeout reports that no callback belonging to this flow arrived in
// time. It names no state, code, or token.
// It says "sign-in" rather than "browser": the same bound covers the pasted
// redirect URL, where no browser ever reaches gcx.
func errCallbackTimeout(d time.Duration) error {
	return fmt.Errorf("timed out after %s waiting for the sign-in to complete", d)
}

// pasteOutcome is what resolvePaste decided about one delivered redirect URL.
// A zero value means "keep waiting": the watcher has already been told why, and
// the run loop goes back to the select.
//
// It is one value rather than a (result, error) pair because "nothing happened,
// carry on" is a third case that neither of those can express.
type pasteOutcome[T any] struct {
	done   bool
	result *T
	err    error
}

// pasteKeepWaiting is the zero outcome, named for readability at the call site.
func pasteKeepWaiting[T any]() pasteOutcome[T] { return pasteOutcome[T]{} }
