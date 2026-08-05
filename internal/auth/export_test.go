package auth

import (
	"context"
	"io"
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
