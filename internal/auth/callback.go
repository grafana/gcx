package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
)

// ErrStateMismatch reports that the callback state does not match the state
// gcx generated. The manual paste path wraps it with its own guidance: there,
// a mismatch nearly always means the URL came from a different login attempt.
//
// The wording deliberately does not name CSRF. A loopback listener is reachable
// by any local process, and in unified login the previous OAuth leg's callback
// often lands on the next leg's port, so a mismatch is far more often a stale
// tab than an attack. Naming an attack made an ordinary failure get reported as
// a security incident.
var ErrStateMismatch = errors.New("callback state does not match this login attempt")

// callbackBelongsToFlow reports whether a callback carries the state that this
// flow generated.
//
// This is the ownership test, and passing it is the only thing that may consume
// a flow's one-shot completion path. Both the HTTP handler and the pasted-URL
// path call it before they claim, so the two cannot drift on what counts as
// "ours". The comparison is constant time: the state is the secret an attacker
// would be guessing.
func callbackBelongsToFlow(q url.Values, expectedState string) bool {
	got := q.Get("state")
	if got == "" || expectedState == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expectedState)) == 1
}

// foreignCallbackPage is shown in the browser for a callback that does not
// belong to the sign-in that is waiting. It names the likely cause instead of
// an attack, and it says the waiting sign-in is unaffected, because it is.
const foreignCallbackPage = "This callback does not belong to the sign-in waiting in your terminal. " +
	"It is most likely from an earlier login attempt or a reloaded tab. " +
	"The sign-in in your terminal is still running — approve it from the URL that terminal printed."

// ignoredCallbackNotice is printed once, by the flow's own goroutine, when a
// request to the callback address was answered and discarded. Without it a
// login that is being probed — or that received a denial the provider failed to
// stamp with our state — shows nothing at all until the deadline.
const ignoredCallbackNotice = "Ignored a request to the callback address that does not belong to this sign-in. Still waiting."

// pasteSupersededNotice explains a pasted URL that arrived after the browser
// callback had already claimed the flow. Every delivered line gets an answer;
// silence here reads as a freeze.
const pasteSupersededNotice = "The browser callback arrived first. Finishing that one."

// callbackError pairs the error reported to the caller with the short message
// rendered on the browser error page. The manual paste path uses only err.
type callbackError struct {
	err  error
	page string
	// spent records that the authorization code was sent to the token
	// endpoint before this error. A spent code can never be redeemed again —
	// the server may already have consumed it, and an authorization server
	// that detects a replay is entitled to revoke whatever it issued
	// (RFC 6819 §4.4.1.1). So a spent failure ends the flow on every path:
	// re-prompting for the same URL could only produce a second exchange of
	// the same code.
	spent bool
}

func (e *callbackError) Error() string { return e.err.Error() }

func (e *callbackError) Unwrap() error { return e.err }

// handleCallbackParams validates the redirect query parameters, exchanges the
// authorization code, and builds a Result.
//
// It is the single code path shared by the HTTP callback handler and the
// manual paste fallback: q comes either from r.URL.Query() or from
// ParseCallbackInput. Keeping one implementation stops the two paths from
// diverging on the state check, the PKCE exchange, or endpoint validation.
func handleCallbackParams(ctx context.Context, q url.Values, expectedState, codeVerifier string) (*Result, *callbackError) {
	// Callers gate on callbackBelongsToFlow before claiming the flow, so this
	// re-check is defence in depth: no caller may reach the exchange with a
	// callback that is not ours.
	if !callbackBelongsToFlow(q, expectedState) {
		return nil, &callbackError{err: ErrStateMismatch, page: foreignCallbackPage}
	}

	if errMsg := q.Get("error"); errMsg != "" {
		errMsg = StripControlChars(errMsg)
		return nil, &callbackError{err: fmt.Errorf("authentication denied: %s", errMsg), page: errMsg}
	}

	code := q.Get("code")
	if code == "" {
		return nil, &callbackError{err: errors.New("no authorization code received"), page: "No authorization code received"}
	}

	endpoint := q.Get("endpoint")
	if endpoint == "" {
		return nil, &callbackError{err: errors.New("no API endpoint received"), page: "No API endpoint received"}
	}
	if err := ValidateEndpointURL(endpoint); err != nil {
		return nil, &callbackError{err: fmt.Errorf("invalid API endpoint: %w", err), page: "Invalid API endpoint"}
	}

	exchangeResult, err := exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("token exchange failed: %w", err), page: "Token exchange failed", spent: true}
	}

	instanceEndpoint := q.Get("instanceEndpoint")
	instanceEndpointURL, err := url.Parse(instanceEndpoint)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("invalid endpoint url: %w", err), page: "Invalid instance endpoint passed", spent: true}
	}
	if instanceEndpointURL.Scheme != "https" {
		return nil, &callbackError{
			err:   fmt.Errorf("invalid endpoint scheme: expected 'https', got '%s'", instanceEndpointURL.Scheme),
			page:  "Invalid instance endpoint: needs to be an HTTPS URL",
			spent: true,
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
func (f *GCOMFlow) handleGCOMCallbackParams(ctx context.Context, q url.Values, expectedState, codeVerifier, redirectURI string) (*GCOMResult, *callbackError) {
	if !callbackBelongsToFlow(q, expectedState) {
		return nil, &callbackError{err: ErrStateMismatch, page: foreignCallbackPage}
	}

	if errMsg := q.Get("error"); errMsg != "" {
		errMsg = StripControlChars(errMsg)
		return nil, &callbackError{err: fmt.Errorf("authentication denied: %s", errMsg), page: errMsg}
	}

	code := q.Get("code")
	if code == "" {
		return nil, &callbackError{err: errors.New("no authorization code received"), page: "No authorization code received"}
	}

	result, err := f.exchangeGCOMToken(ctx, code, codeVerifier, redirectURI)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("token exchange failed: %w", err), page: "Token exchange failed", spent: true}
	}

	return result, nil
}
