package secrets

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const redactedQuery = "REDACTED"

type redactURLQueryKey struct{}

// WithRedactedURLQuery marks HTTP requests created with ctx as containing a sensitive
// query string that must not be written to logs.
func WithRedactedURLQuery(ctx context.Context) context.Context {
	return context.WithValue(ctx, redactURLQueryKey{}, true)
}

// URLString returns the request URL in a form that is safe to log.
func URLString(ctx context.Context, u *url.URL) string {
	if u == nil {
		return ""
	}
	if redact, _ := ctx.Value(redactURLQueryKey{}).(bool); !redact || u.RawQuery == "" {
		return u.String()
	}
	clone := *u
	clone.RawQuery = redactedQuery
	clone.ForceQuery = false
	return clone.String()
}

// ErrorString returns err's message with occurrences of the request URL and
// raw query replaced by their safe-to-log forms.
func ErrorString(ctx context.Context, u *url.URL, err error) string {
	if err == nil {
		return ""
	}
	if u == nil {
		return err.Error()
	}
	if redact, _ := ctx.Value(redactURLQueryKey{}).(bool); !redact || u.RawQuery == "" {
		return err.Error()
	}
	message := strings.ReplaceAll(err.Error(), u.String(), URLString(ctx, u))
	message = strings.ReplaceAll(message, u.RawQuery, redactedQuery)
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || urlErr.URL == "" {
		return message
	}
	errorURL, parseErr := url.Parse(urlErr.URL)
	if parseErr != nil || errorURL.RawQuery == "" {
		return message
	}
	message = strings.ReplaceAll(message, urlErr.URL, URLString(ctx, errorURL))
	return strings.ReplaceAll(message, errorURL.RawQuery, redactedQuery)
}

// Request returns a shallow clone whose URL is safe for request-dump logging.
// The original request, including its complete query string, is unchanged.
func Request(req *http.Request) *http.Request {
	if req == nil || req.URL == nil {
		return req
	}
	if redact, _ := req.Context().Value(redactURLQueryKey{}).(bool); !redact || req.URL.RawQuery == "" {
		return req
	}
	clone := req.Clone(req.Context())
	urlClone := *req.URL
	urlClone.RawQuery = redactedQuery
	urlClone.ForceQuery = false
	clone.URL = &urlClone
	return clone
}
