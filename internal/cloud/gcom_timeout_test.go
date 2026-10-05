package cloud_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timeoutProbeTransport func(*http.Request) (*http.Response, error)

func (f timeoutProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGCOMClient_OperationTimeouts(t *testing.T) {
	client, err := cloud.NewGCOMClient("https://example.com", "test-token")
	require.NoError(t, err)
	for _, tc := range []struct {
		name          string
		create        bool
		callerTimeout time.Duration
		want          time.Duration
	}{
		{name: "read before create", want: 30 * time.Second},
		{name: "create", create: true, want: 2 * time.Minute},
		{name: "read after create", want: 30 * time.Second},
		{name: "shorter caller deadline", create: true, callerTimeout: 5 * time.Second, want: 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			cloud.HTTPClientForTest(client).Transport = timeoutProbeTransport(func(req *http.Request) (*http.Response, error) {
				called = true
				deadline, ok := req.Context().Deadline()
				require.True(t, ok)
				remaining := time.Until(deadline)
				assert.LessOrEqual(t, remaining, tc.want)
				assert.Greater(t, remaining, tc.want-time.Second)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})
			ctx := context.Background()
			if tc.callerTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.callerTimeout)
				defer cancel()
			}
			if tc.create {
				_, err = client.CreateStack(ctx, cloud.CreateStackRequest{Org: "example-org", Name: "demo", Slug: "demo"})
			} else {
				_, err = client.GetStack(ctx, "demo")
			}
			require.NoError(t, err)
			assert.True(t, called)
			assert.Equal(t, 30*time.Second, cloud.HTTPClientForTest(client).Timeout)
		})
	}
}
