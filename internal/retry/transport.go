package retry

import (
	"errors"
	"io"
	"math"
	"math/rand/v2" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- jitter does not need crypto randomness
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-app-sdk/logging"
)

// Transport wraps an http.RoundTripper and retries on rate-limiting (429)
// and transient server errors (502, 503, 504). It respects the Retry-After
// header and uses exponential backoff with full jitter.
//
// Idempotent methods (GET, HEAD, PUT, DELETE, OPTIONS) are retried on both
// 429 and 5xx. Non-idempotent methods (POST, PATCH) are only retried on 429
// (where the server explicitly rejected the request) and connection errors
// (where the request likely was not received).
type Transport struct {
	// Base is the underlying RoundTripper. If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	// MaxRetries is the maximum number of retry attempts (not counting the
	// initial request). Defaults to 3.
	MaxRetries int

	// MinBackoff is the minimum backoff duration before the first retry.
	// Defaults to 500ms.
	MinBackoff time.Duration

	// MaxBackoff is the maximum backoff duration for any single retry.
	// Defaults to 10s.
	MaxBackoff time.Duration

	// MaxRetryAfter caps how long the transport will wait when a server sends
	// a Retry-After header. Defaults to 30s.
	MaxRetryAfter time.Duration
}

const (
	defaultMaxRetries    = 3
	defaultMinBackoff    = 500 * time.Millisecond
	defaultMaxBackoff    = 10 * time.Second
	defaultMaxRetryAfter = 30 * time.Second
)

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *Transport) maxRetries() int {
	if t.MaxRetries > 0 {
		return t.MaxRetries
	}
	return defaultMaxRetries
}

func (t *Transport) minBackoff() time.Duration {
	if t.MinBackoff > 0 {
		return t.MinBackoff
	}
	return defaultMinBackoff
}

func (t *Transport) maxBackoff() time.Duration {
	if t.MaxBackoff > 0 {
		return t.MaxBackoff
	}
	return defaultMaxBackoff
}

func (t *Transport) maxRetryAfter() time.Duration {
	if t.MaxRetryAfter > 0 {
		return t.MaxRetryAfter
	}
	return defaultMaxRetryAfter
}

// RoundTrip executes the request with retry logic for transient failures.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	maxRetries := t.maxRetries()

	// Track whether the original request had a body that needs replaying.
	hasBody := req.Body != nil

	for attempt := 0; ; attempt++ {
		// Reset the request body for retries.
		if attempt > 0 && hasBody {
			if req.GetBody == nil {
				// Cannot replay the body — return the last response/error.
				return resp, err
			}
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return resp, err
			}
			req.Body = body
		}

		resp, err = t.base().RoundTrip(req)

		if !t.shouldRetry(req, resp, err, attempt, maxRetries) {
			return resp, err
		}

		// Drain and close the response body so the TCP connection can be reused.
		if resp != nil {
			drainAndClose(resp.Body)
		}

		backoff := t.computeBackoff(attempt, resp)

		logger := logging.FromContext(req.Context())
		attrs := []any{
			"method", req.Method,
			"url", req.URL.String(),
			"attempt", attempt + 1,
			"max_retries", maxRetries,
			"backoff", backoff.String(),
		}
		if resp != nil {
			attrs = append(attrs, "status", resp.StatusCode)
		}
		if err != nil {
			attrs = append(attrs, "error", err.Error())
		}
		logger.Warn("retrying HTTP request", attrs...)

		timer := time.NewTimer(backoff)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

// shouldRetry determines whether a request should be retried based on the
// response status code, error, HTTP method, and attempt number.
func (t *Transport) shouldRetry(req *http.Request, resp *http.Response, err error, attempt, maxRetries int) bool {
	if attempt >= maxRetries {
		return false
	}
	if req.Context().Err() != nil {
		return false
	}

	// For retries that require a body replay, the body must be rewindable.
	// Requests without a body (e.g. GET) can always be retried.
	if req.Body != nil && req.GetBody == nil {
		return false
	}

	// Connection/network errors: retry all methods.
	if err != nil {
		return isTransientConnectionError(err)
	}

	// 429 Too Many Requests: always retry regardless of method.
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}

	// 502/503/504: only retry idempotent methods.
	if resp.StatusCode == http.StatusBadGateway ||
		resp.StatusCode == http.StatusServiceUnavailable ||
		resp.StatusCode == http.StatusGatewayTimeout {
		return isIdempotent(req.Method)
	}

	return false
}

// computeBackoff returns the duration to wait before the next retry.
// If the response contains a Retry-After header, that value is used
// (capped at MaxRetryAfter). Otherwise, exponential backoff with full
// jitter is used.
func (t *Transport) computeBackoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
			if maxRA := t.maxRetryAfter(); ra > maxRA {
				ra = maxRA
			}
			return ra
		}
	}

	// Exponential backoff with full jitter.
	expBackoff := float64(t.minBackoff()) * math.Pow(2, float64(attempt))
	capped := math.Min(expBackoff, float64(t.maxBackoff()))
	jittered := time.Duration(rand.Int64N(int64(capped) + 1)) //nolint:gosec // Jitter does not need crypto randomness.
	return max(jittered, t.minBackoff())
}

// parseRetryAfter parses a Retry-After header value, which can be either
// an integer number of seconds or an HTTP-date (RFC 7231 §7.1.3).
func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 0
	}

	// Try integer seconds first.
	if secs, err := strconv.ParseInt(val, 10, 64); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}

	// Try HTTP-date (RFC 7231, uses RFC 1123 format).
	if t, err := time.Parse(time.RFC1123, val); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return 0
}

// isIdempotent returns true for HTTP methods that are safe to retry on
// server errors, since repeating them does not cause additional side effects.
func isIdempotent(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

// isTransientConnectionError checks whether an error is a transient network
// error that is worth retrying (connection refused, reset, DNS temporary).
//
// Concrete types are checked before the net.Error interface because both
// *net.OpError and *net.DNSError implement net.Error. Checking the interface
// first would match them and return netErr.Timeout() — which is false for
// connection-refused errors, silently skipping the retry.
func isTransientConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.OpError wrapping syscall-level connection errors
	// (ECONNREFUSED, ECONNRESET, etc.).
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Check for net.DNSError with IsTemporary.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTemporary
	}

	// Fallback: check for net.Error interface (timeout, temporary).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

// drainAndClose reads up to 4KB from the body and closes it, allowing the
// underlying TCP connection to be reused.
func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	io.CopyN(io.Discard, body, 4096) //nolint:errcheck // Best-effort drain for connection reuse.
	body.Close()
}
