package auth

import (
	"context"
	"io"
	"net/url"
	"os"
	"time"
)

// ExchangeCodeForToken exposes the unexported exchangeCodeForToken for black-box tests.
func ExchangeCodeForToken(ctx context.Context, endpoint, code, codeVerifier string) (any, error) {
	return exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
}

// DefaultCallbackAcquireTimeout exposes the production wait bound.
const DefaultCallbackAcquireTimeout = defaultCallbackAcquireTimeout

// SetAcquireTimeout shortens one flow's callback wait. It is a per-instance
// setter rather than a swappable package variable, so concurrent tests cannot
// interfere with each other.
func SetAcquireTimeout(f *Flow, d time.Duration) { f.acquireTimeout = d }

// SetGCOMAcquireTimeout is SetAcquireTimeout for the grafana.com flow.
func SetGCOMAcquireTimeout(f *GCOMFlow, d time.Duration) { f.acquireTimeout = d }

// CallbackArbiter exposes the ownership arbiter for direct, deterministic tests
// of the concurrency contract that the live flows depend on.
type CallbackArbiter = callbackArbiter

// CallbackClaim exposes the arbiter's verdict type.
type CallbackClaim = callbackClaim

// Arbiter claim verdicts.
const (
	ClaimGranted = claimGranted
	ClaimTaken   = claimTaken
	ClaimExpired = claimExpired
)

// NewCallbackArbiter builds an arbiter with an explicit deadline.
func NewCallbackArbiter(timeout time.Duration) *CallbackArbiter { return newCallbackArbiter(timeout) }

// Claim, Settle, Release, Expired and Stop expose the arbiter's contract.
func (a *callbackArbiter) Claim() CallbackClaim     { return a.claim() }
func (a *callbackArbiter) Settle()                  { a.settle() }
func (a *callbackArbiter) Release()                 { a.release() }
func (a *callbackArbiter) Expired() <-chan struct{} { return a.expired() }
func (a *callbackArbiter) Stop()                    { a.stop() }
func (a *callbackArbiter) DeadlineReached()         { a.deadlineReached() }
func (a *callbackArbiter) IsExchanging() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state == arbiterExchanging
}

// CallbackBelongsToFlow exposes the ownership test shared by the HTTP handler
// and the paste path.
func CallbackBelongsToFlow(q url.Values, expectedState string) bool {
	return callbackBelongsToFlow(q, expectedState)
}

// PasteDisposition and its values expose the paste path's claim decision.
type PasteDisposition = pasteDisposition

const (
	PasteForeign    = pasteForeign
	PasteSuperseded = pasteSuperseded
	PasteClaimed    = pasteClaimed
)

// ClaimPastedCallback exposes the real paste-path gate.
func ClaimPastedCallback(arb *CallbackArbiter, values url.Values, expectedState string) PasteDisposition {
	return claimPastedCallback(arb, values, expectedState)
}

// ForeignCallbackPage exposes the browser copy for a callback that is not ours.
const ForeignCallbackPage = foreignCallbackPage

// ManualCallbackPort exposes the fixed manual-mode callback port.
const ManualCallbackPort = manualCallbackPort

// ReadLine exposes the unexported readLine for black-box tests.
func ReadLine(r io.Reader) (string, error) {
	return readLine(r)
}

// PrintRemoteSessionHint exposes the unexported printRemoteSessionHint.
func PrintRemoteSessionHint(w io.Writer, port int, command string) {
	printRemoteSessionHint(w, port, command)
}

// PasteWatcher exposes the unexported watcher type for black-box tests.
type PasteWatcher = pasteWatcher

// PastedInput exposes one watcher delivery for black-box tests.
type PastedInput = pastedInput

// StartPasteWatcher exposes the unexported startPasteWatcher. It returns nil
// when the paste path does not apply.
func StartPasteWatcher(w io.Writer, port int) *PasteWatcher {
	return startPasteWatcher(w, port)
}

// SwapPasteTerminal replaces the terminal opener so tests can drive the watcher
// with a pipe. A pipe is pollable, like /dev/tty, so it exercises the same
// Close-unblocks-Read teardown. It returns a restore function.
func SwapPasteTerminal(f *os.File, ok bool) func() {
	previous := openPasteTerminal
	openPasteTerminal = func() (*os.File, bool) { return f, ok }
	return func() { openPasteTerminal = previous }
}

// OpenPasteTerminal exposes the real controlling-terminal opener so a test can
// verify that a pending read on it is actually cancellable.
func OpenPasteTerminal() (*os.File, bool) {
	return openPasteTerminal()
}
