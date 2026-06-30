package auth

import (
	"context"
	"os/exec"
)

// ExchangeCodeForToken exposes the unexported exchangeCodeForToken for black-box tests.
func ExchangeCodeForToken(ctx context.Context, endpoint, code, codeVerifier string) (any, error) {
	return exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
}

// BrowserCommand exposes the unexported browserCommand for black-box tests.
func BrowserCommand(ctx context.Context, goos, url string) (*exec.Cmd, error) {
	return browserCommand(ctx, goos, url)
}
