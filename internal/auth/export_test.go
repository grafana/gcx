package auth

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
)

// ExchangeCodeForToken exposes the unexported exchangeCodeForToken for black-box tests.
func ExchangeCodeForToken(ctx context.Context, endpoint, code, codeVerifier string) (any, error) {
	return exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
}

// ManualCallbackPort exposes the fixed manual-mode callback port.
const ManualCallbackPort = manualCallbackPort

// ManualPasteTries exposes the bound on the manual retry loop.
const ManualPasteTries = manualPasteTries

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

// SwapOpenBrowser replaces the browser opener so a test can record the URLs a
// flow opens without starting a browser. It returns a restore function.
func SwapOpenBrowser(open func(string) (bool, error)) func() {
	previous := openBrowser
	openBrowser = open
	return func() { openBrowser = previous }
}

// StartReopenWatcher exposes the unexported startReopenWatcher. It returns nil
// when the reopen shortcut does not apply.
func StartReopenWatcher(w io.Writer) *PasteWatcher {
	return startReopenWatcher(w)
}

// AuthAndEntryURLs exposes the consent URL a flow builds for a callback port,
// and the URL that the browser opens first.
func (f *Flow) AuthAndEntryURLs(port int, state, codeChallenge string) (string, string) {
	authURL := f.buildAuthURL(port, state, codeChallenge)
	return authURL, f.buildEntryURL(authURL)
}

// ReopenRequestForTest returns a watcher whose only delivery is one reopen
// request, already waiting.
func ReopenRequestForTest() *PasteWatcher {
	w := &pasteWatcher{
		values: make(chan pastedInput, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		reopen: true,
	}
	w.values <- pastedInput{Reopen: true}
	close(w.done)
	return w
}

// AwaitReadyResult runs the flow's wait loop with a result that is already
// waiting alongside paste's delivery.
func AwaitReadyResult(ctx context.Context, paste *PasteWatcher, result *Result, reopen func()) (*Result, error) {
	resultCh := make(chan *Result, 1)
	resultCh <- result
	return awaitCallbackOrPaste(ctx, io.Discard, paste, reopen, resultCh, make(chan error),
		func(url.Values) (*Result, *callbackError) { return nil, nil })
}

// TerminalDeviceCandidates exposes the device-path filter of the macOS
// terminal fallback.
func TerminalDeviceCandidates(devNames, ptsPaths []string) []string {
	return terminalDeviceCandidates(devNames, ptsPaths)
}

// StartGCOMCallbackServer starts the grafana.com flow's callback server on
// listener, so a test can send it callbacks that never reach a token exchange.
// It returns the channel on which the server reports a login-ending error.
func StartGCOMCallbackServer(ctx context.Context, listener net.Listener, state string) (*http.Server, <-chan error) {
	f := NewGCOMFlow(GCOMOptions{ClientID: "gcx", GCOMURL: "https://grafana.com", Writer: io.Discard})
	errCh := make(chan error, 1)
	server := f.startGCOMCallbackServer(ctx, listener, state, "verifier", "http://127.0.0.1/callback", &exchangeGuard{}, make(chan *GCOMResult, 1), errCh)
	return server, errCh
}
