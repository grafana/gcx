package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync/atomic"
)

// errStateMismatch reports that the callback state does not match the state
// gcx generated. The manual paste path replaces it with its own guidance:
// there, a mismatch nearly always means the URL came from a different login
// attempt.
var errStateMismatch = errors.New("invalid state - possible CSRF attack")

// callbackError pairs the error reported to the caller with the short message
// rendered on the browser error page. The manual paste path uses only err.
type callbackError struct {
	err  error
	page string
}

// errExchangeClaimed reports that the other route already exchanged the
// authorization code. No caller shows this error to the user: the route that
// won the race delivers the result.
var errExchangeClaimed = errors.New("another route already exchanged the authorization code")

// exchangeGuard lets one route exchange the authorization code. The callback
// server and the paste reader accept the same code, and the code is single-use,
// so a second exchange always fails. Without the guard the loser reported that
// failure to the user, one moment before the winner reported success.
//
// A nil guard always grants the claim. The manual paste flow runs no callback
// server, so it has no race to guard.
type exchangeGuard struct{ taken atomic.Bool }

// claim reports whether the caller may run the token exchange. Call it after
// the state check and the code check, never before: a pasted URL from an older
// attempt must not consume the claim that the real callback needs.
func (g *exchangeGuard) claim() bool {
	if g == nil {
		return true
	}
	return g.taken.CompareAndSwap(false, true)
}

// checkCallbackBasics runs the three checks that every flow shares: the state
// must match, the provider must report no error, and an authorization code must
// be present. It returns that code.
func checkCallbackBasics(q url.Values, expectedState string) (string, *callbackError) {
	if q.Get("state") != expectedState {
		return "", &callbackError{err: errStateMismatch, page: "Invalid state parameter"}
	}

	if errMsg := q.Get("error"); errMsg != "" {
		errMsg = StripControlChars(errMsg)
		return "", &callbackError{err: fmt.Errorf("authentication denied: %s", errMsg), page: errMsg}
	}

	code := q.Get("code")
	if code == "" {
		return "", &callbackError{err: errors.New("no authorization code received"), page: "No authorization code received"}
	}

	return code, nil
}

// paramHandler runs the semantic checks and the token exchange for one flow.
// The two shared helpers below take it, so the plugin flow and the grafana.com
// flow keep one implementation of the paste behaviour.
type paramHandler[T any] func(url.Values) (*T, *callbackError)

// manualPasteTries bounds the number of redirect URLs that the manual flow
// accepts. A typo must not cost a full re-run for a fresh state and a fresh
// code, and a bound keeps the loop finite for a reader that is not a terminal.
const manualPasteTries = 3

// runManualPaste completes a flow without a callback server. The browser cannot
// reach the callback address, so the user copies the redirect URL out of the
// address bar and pastes it here.
//
// A URL that fails a check re-prompts, up to manualPasteTries lines. A read
// error ends the flow at once, because the next read reports it again.
//
// Pass an empty verification string for a flow that shows no verification code.
func runManualPaste[T any](
	ctx context.Context,
	w io.Writer,
	r io.Reader,
	authURL, verification string,
	handle paramHandler[T],
) (*T, error) {
	printManualInstructions(w, authURL, verification)

	// A pasted line is on screen from the first read on, so the notice belongs
	// on every return below, not on the success return alone. A state mismatch
	// is the case that leaves the most on screen.
	pasteSeen := false
	defer func() {
		if pasteSeen {
			fmt.Fprintln(w, manualCallbackHygieneNotice)
		}
	}()

	var lastErr error
	for try := 1; try <= manualPasteTries; try++ {
		line, err := readLineContext(ctx, r)
		if err != nil {
			// The reader ended or the context stopped the wait. Report the
			// failure of the URL that the user did paste: the end of the input
			// is a consequence of it, and it says less about the cause.
			if lastErr != nil && ctx.Err() == nil {
				return nil, lastErr
			}
			return nil, err
		}
		pasteSeen = true

		values, parseErr := ParseCallbackInput(line)
		if parseErr != nil {
			lastErr = fmt.Errorf("cannot read the redirect URL: %w", parseErr)
		} else {
			result, cerr := handle(values)
			if cerr == nil {
				return result, nil
			}
			lastErr = pasteRejection(cerr.err)
		}

		if try < manualPasteTries {
			printPasteRejection(w, lastErr, manualRedirectPrompt)
		}
	}
	return nil, lastErr
}

// awaitCallbackOrPaste waits for whichever route completes first: the callback
// server, or a redirect URL that the user pastes. A pasted URL that fails the
// semantic checks re-prompts, because the callback server still listens.
func awaitCallbackOrPaste[T any](
	ctx context.Context,
	w io.Writer,
	paste *pasteWatcher,
	resultCh <-chan *T,
	errCh <-chan error,
	handle paramHandler[T],
) (*T, error) {
	// The notice covers every return once a URL is on screen, including the
	// failure returns and the case where the callback wins after a paste.
	pasteSeen := false
	defer func() {
		if pasteSeen {
			fmt.Fprintln(w, manualCallbackHygieneNotice)
		}
	}()

	for {
		select {
		case result := <-resultCh:
			return result, nil
		case err := <-errCh:
			return nil, err
		case pasted := <-paste.Input():
			if pasted.Closed {
				fmt.Fprintln(w, "\nThe paste route ended. gcx still waits for the browser.")
				continue
			}
			pasteSeen = true
			if pasted.Err != nil {
				paste.Reject(pasted.Err)
				continue
			}
			result, cerr := handle(pasted.Values)
			if cerr != nil {
				if errors.Is(cerr.err, errExchangeClaimed) {
					// The callback server won the race. Its result is already on
					// its way, so say nothing and let the next round deliver it.
					continue
				}
				paste.Reject(pasteRejection(cerr.err))
				continue
			}
			return result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// handleCallbackParams validates the redirect query parameters, exchanges the
// authorization code, and builds a Result.
//
// It is the single code path shared by the HTTP callback handler and the
// manual paste fallback: q comes either from r.URL.Query() or from
// ParseCallbackInput. Keeping one implementation stops the two paths from
// diverging on the state check, the PKCE exchange, or endpoint validation.
func handleCallbackParams(ctx context.Context, q url.Values, expectedState, codeVerifier string, guard *exchangeGuard) (*Result, *callbackError) {
	code, cerr := checkCallbackBasics(q, expectedState)
	if cerr != nil {
		return nil, cerr
	}

	endpoint := q.Get("endpoint")
	if endpoint == "" {
		return nil, &callbackError{err: errors.New("no API endpoint received"), page: "No API endpoint received"}
	}
	if err := ValidateEndpointURL(endpoint); err != nil {
		return nil, &callbackError{err: fmt.Errorf("invalid API endpoint: %w", err), page: "Invalid API endpoint"}
	}

	if !guard.claim() {
		return nil, &callbackError{err: errExchangeClaimed, page: "The login already completed in the terminal."}
	}

	exchangeResult, err := exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("token exchange failed: %w", err), page: "Token exchange failed"}
	}

	instanceEndpoint := q.Get("instanceEndpoint")
	instanceEndpointURL, err := url.Parse(instanceEndpoint)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("invalid endpoint url: %w", err), page: "Invalid instance endpoint passed"}
	}
	if instanceEndpointURL.Scheme != "https" {
		return nil, &callbackError{
			err:  fmt.Errorf("invalid endpoint scheme: expected 'https', got '%s'", instanceEndpointURL.Scheme),
			page: "Invalid instance endpoint: needs to be an HTTPS URL",
		}
	}

	return &Result{
		Token:            exchangeResult.Data.Token,
		Email:            exchangeResult.Data.Email,
		DeviceName:       q.Get("device"),
		APIEndpoint:      exchangeResult.Data.APIEndpoint,
		ExpiresAt:        exchangeResult.Data.ExpiresAt,
		RefreshToken:     exchangeResult.Data.RefreshToken,
		RefreshExpiresAt: exchangeResult.Data.RefreshExpiresAt,
		InstanceEndpoint: instanceEndpoint,
	}, nil
}

// handleGCOMCallbackParams is the grafana.com equivalent of
// handleCallbackParams. redirectURI must be byte-identical to the redirect_uri
// sent on the authorize request, because GCOM rejects the exchange otherwise.
func (f *GCOMFlow) handleGCOMCallbackParams(ctx context.Context, q url.Values, expectedState, codeVerifier, redirectURI string, guard *exchangeGuard) (*GCOMResult, *callbackError) {
	code, cerr := checkCallbackBasics(q, expectedState)
	if cerr != nil {
		return nil, cerr
	}

	if !guard.claim() {
		return nil, &callbackError{err: errExchangeClaimed, page: "The login already completed in the terminal."}
	}

	result, err := f.exchangeGCOMToken(ctx, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("token exchange failed: %w", err), page: "Token exchange failed"}
	}

	return result, nil
}
