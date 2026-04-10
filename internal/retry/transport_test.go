package retry_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGetRequest creates a GET request with the test context.
func newGetRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	return req
}

// newPostRequest creates a POST request with the test context and a JSON body.
func newPostRequest(t *testing.T, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestTransport_429WithRetryAfterSeconds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:          http.DefaultTransport,
			MinBackoff:    10 * time.Millisecond,
			MaxBackoff:    50 * time.Millisecond,
			MaxRetryAfter: 50 * time.Millisecond,
		},
	}

	resp, err := client.Do(newGetRequest(t, srv.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestTransport_429WithRetryAfterHTTPDate(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			retryAt := time.Now().Add(50 * time.Millisecond).UTC().Format(time.RFC1123)
			w.Header().Set("Retry-After", retryAt)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:          http.DefaultTransport,
			MinBackoff:    10 * time.Millisecond,
			MaxRetryAfter: 2 * time.Second,
		},
	}

	resp, err := client.Do(newGetRequest(t, srv.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestTransport_429WithoutRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
		},
	}

	resp, err := client.Do(newGetRequest(t, srv.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestTransport_TransientServerError(t *testing.T) {
	for _, code := range []int{502, 503, 504} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) <= 2 {
					w.WriteHeader(code)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			client := &http.Client{
				Transport: &retry.Transport{
					Base:       http.DefaultTransport,
					MinBackoff: 1 * time.Millisecond,
					MaxBackoff: 5 * time.Millisecond,
				},
			}

			resp, err := client.Do(newGetRequest(t, srv.URL))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, int32(3), attempts.Load())
		})
	}
}

func TestTransport_MaxRetriesExhausted(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MaxRetries: 2,
			MinBackoff: 1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
		},
	}

	resp, err := client.Do(newGetRequest(t, srv.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	// 1 initial + 2 retries = 3 total.
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestTransport_NonRetryableStatusCodes(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 409, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			client := &http.Client{
				Transport: &retry.Transport{
					Base:       http.DefaultTransport,
					MinBackoff: 1 * time.Millisecond,
				},
			}

			resp, err := client.Do(newGetRequest(t, srv.URL))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, code, resp.StatusCode)
			assert.Equal(t, int32(1), attempts.Load(), "should not retry %d", code)
		})
	}
}

func TestTransport_POSTNotRetriedOn503(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
		},
	}

	resp, err := client.Do(newPostRequest(t, srv.URL, strings.NewReader(`{}`)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load(), "POST should not retry on 503")
}

func TestTransport_POSTRetriedOn429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
		},
	}

	resp, err := client.Do(newPostRequest(t, srv.URL, strings.NewReader(`{"key":"value"}`)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), attempts.Load(), "POST should retry on 429")
}

func TestTransport_ContextCancellation(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 500 * time.Millisecond, // Long enough to cancel during wait.
			MaxBackoff: 1 * time.Second,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	// Cancel after a short delay so the first retry's backoff is interrupted.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	resp, respErr := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, respErr)
	assert.Equal(t, int32(1), attempts.Load(), "should stop after context cancellation")
}

func TestTransport_BodyReplayedCorrectly(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
		},
	}

	payload := `{"metric":"test","value":42}`
	resp, err := client.Do(newPostRequest(t, srv.URL, strings.NewReader(payload)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, bodies, 2)
	assert.Equal(t, payload, bodies[0], "first attempt body")
	assert.Equal(t, payload, bodies[1], "retry body must match")
}

func TestTransport_NoGetBodySkipsRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
		},
	}

	// Use a custom body that does not set GetBody (pipe reader).
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("data")) //nolint:errcheck
		pw.Close()
	}()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, pr)
	require.NoError(t, err)
	// Explicitly clear GetBody to simulate a streaming body.
	req.GetBody = nil

	resp, respErr := client.Do(req)
	require.NoError(t, respErr)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load(), "should not retry without GetBody")
}

func TestTransport_ResponseBodyDrainedOnRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited, please slow down")) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success")) //nolint:errcheck
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
		},
	}

	resp, err := client.Do(newGetRequest(t, srv.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "success", string(body))
}

func TestTransport_DefaultsApplied(t *testing.T) {
	// A zero-value Transport uses sensible defaults internally.
	// DefaultMaxRetries is exported via export_test.go for validation.
	assert.Equal(t, 3, retry.DefaultMaxRetries)
	assert.Equal(t, 500*time.Millisecond, retry.DefaultMinBackoff)
	assert.Equal(t, 10*time.Second, retry.DefaultMaxBackoff)
	assert.Equal(t, 30*time.Second, retry.DefaultMaxRetryAfter)
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want time.Duration
	}{
		{name: "empty", val: "", want: 0},
		{name: "integer seconds", val: "3", want: 3 * time.Second},
		{name: "zero seconds", val: "0", want: 0},
		{name: "negative", val: "-1", want: 0},
		{name: "garbage", val: "not-a-number", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retry.ParseRetryAfter(tt.val)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsIdempotent(t *testing.T) {
	idempotent := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS"}
	for _, m := range idempotent {
		assert.True(t, retry.IsIdempotent(m), "%s should be idempotent", m)
	}

	nonIdempotent := []string{"POST", "PATCH"}
	for _, m := range nonIdempotent {
		assert.False(t, retry.IsIdempotent(m), "%s should not be idempotent", m)
	}
}

func TestTransport_BytesBufferBodyGetBody(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if attempts.Add(1) == 1 {
			assert.Equal(t, "hello", string(b))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		assert.Equal(t, "hello", string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &retry.Transport{
			Base:       http.DefaultTransport,
			MinBackoff: 1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
		},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, bytes.NewBufferString("hello"))
	require.NoError(t, err)

	resp, respErr := client.Do(req)
	require.NoError(t, respErr)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), attempts.Load())
}
