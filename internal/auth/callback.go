package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// ErrStateMismatch reports that the callback state does not match the state
// gcx generated. The manual paste path wraps it with its own guidance: there,
// a mismatch nearly always means the URL came from a different login attempt.
var ErrStateMismatch = errors.New("invalid state - possible CSRF attack")

// callbackError pairs the error reported to the caller with the short message
// rendered on the browser error page. The manual paste path uses only err.
type callbackError struct {
	err  error
	page string
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
	if q.Get("state") != expectedState {
		return nil, &callbackError{err: ErrStateMismatch, page: "Invalid state parameter"}
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
func (f *GCOMFlow) handleGCOMCallbackParams(ctx context.Context, q url.Values, expectedState, codeVerifier, redirectURI string) (*GCOMResult, *callbackError) {
	if q.Get("state") != expectedState {
		return nil, &callbackError{err: ErrStateMismatch, page: "Invalid state parameter"}
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
		return nil, &callbackError{err: fmt.Errorf("token exchange failed: %w", err), page: "Token exchange failed"}
	}

	return result, nil
}
