package auth

import (
	"context"
	"io"
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
