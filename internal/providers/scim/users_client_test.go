package scim //nolint:testpackage // Tests check the SCIM request boundary with the shared retry transport.

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/require"
)

type userRoundTripFunc func(*http.Request) (*http.Response, error)

func (f userRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUserWritesMakeOneAttempt(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		for _, failure := range []string{"connection reset after write", "rate limit"} {
			t.Run(method+"/"+failure, func(t *testing.T) {
				attempts := 0
				transport := userRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					attempts++
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"active":false}`, string(body))
					if attempts == 1 {
						if failure == "connection reset after write" {
							return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
						}
						return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"detail":"rate limit"}`))}, nil
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
				})
				client := usersClient{base: "https://example.grafana.net", token: "test-token", http: &http.Client{Transport: &retry.Transport{Base: transport, MinBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond}}}
				_, err := client.request(context.Background(), method, "/1", nil, map[string]any{"active": false})
				require.Error(t, err)
				require.Equal(t, 1, attempts)
			})
		}
	}
}

func TestUserReadsKeepRetries(t *testing.T) {
	attempts := 0
	transport := userRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"1"}`))}, nil
	})
	client := usersClient{base: "https://example.grafana.net", token: "test-token", http: &http.Client{Transport: &retry.Transport{Base: transport, MinBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond}}}
	user, err := client.get(context.Background(), "1")
	require.NoError(t, err)
	require.Equal(t, "1", user["id"])
	require.Equal(t, 2, attempts)
}
