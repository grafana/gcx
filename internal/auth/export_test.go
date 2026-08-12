package auth

import (
	"context"
	"io"
	"net/url"
	"os"
)

// ExchangeCodeForToken exposes the unexported exchangeCodeForToken for black-box tests.
func ExchangeCodeForToken(ctx context.Context, endpoint, code, codeVerifier string) (any, error) {
	return exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
}

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

// FlushTerminalInput exposes the terminal input flush that Close runs.
func FlushTerminalInput(f *os.File) error {
	return flushTerminalInput(f)
}

// ExchangeGuard exposes the single-use guard for black-box tests.
type ExchangeGuard = exchangeGuard

// ClaimExchange exposes the claim on the guard. A nil guard always grants it.
func ClaimExchange(g *ExchangeGuard) bool {
	return g.claim()
}

// ErrExchangeClaimed exposes the sentinel that the losing route returns.
var ErrExchangeClaimed = errExchangeClaimed

// ErrStateMismatch exposes the CSRF sentinel for black-box tests.
var ErrStateMismatch = errStateMismatch

// HandleCallbackParams exposes the shared parameter handler so a test can prove
// that a taken guard stops the second token exchange.
func HandleCallbackParams(ctx context.Context, q url.Values, expectedState, codeVerifier string, guard *ExchangeGuard) error {
	_, cerr := handleCallbackParams(ctx, q, expectedState, codeVerifier, guard)
	if cerr == nil {
		return nil
	}
	return cerr.err
}
