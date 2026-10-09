package providers_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatError(t *testing.T) {
	tests := []struct {
		name          string
		code          int
		body          string
		want          string
		serverMessage string
		traceID       string
	}{
		{
			name:          "json message",
			code:          400,
			body:          `{"message":"bad request data"}`,
			want:          "request failed with status 400: bad request data",
			serverMessage: "bad request data",
		},
		{
			name:          "json message with traceID",
			code:          400,
			body:          `{"message":"bad request data","traceID":"abc123"}`,
			want:          "request failed with status 400: bad request data (traceID abc123)",
			serverMessage: "bad request data",
			traceID:       "abc123",
		},
		{
			name:          "error field preferred over message",
			code:          500,
			body:          `{"error":"boom","message":"ignored"}`,
			want:          "request failed with status 500: boom",
			serverMessage: "boom",
		},
		{
			name:          "err detail preferred over generic msg",
			code:          400,
			body:          `{"msg":"Invalid incoming check","err":"browser checks require channels.k6.id"}`,
			want:          "request failed with status 400: browser checks require channels.k6.id",
			serverMessage: "browser checks require channels.k6.id",
		},
		{
			name:          "non-string err preserves msg fallback",
			code:          400,
			body:          `{"msg":"bad request data","err":{"field":"job"}}`,
			want:          "request failed with status 400: bad request data",
			serverMessage: "bad request data",
		},
		{
			name:          "raw body fallback",
			code:          502,
			body:          "upstream unavailable",
			want:          "request failed with status 502: upstream unavailable",
			serverMessage: "upstream unavailable",
		},
		{
			name: "empty body",
			code: 503,
			body: "",
			want: "request failed with status 503",
		},
		{
			name:          "json without message retains raw body and trace",
			code:          500,
			body:          `{"reason":"InternalError","traceID":"abc123"}`,
			want:          `request failed with status 500: {"reason":"InternalError","traceID":"abc123"}`,
			serverMessage: `{"reason":"InternalError","traceID":"abc123"}`,
			traceID:       "abc123",
		},
		{
			name:          "malformed JSON retains raw body",
			code:          500,
			body:          `{"message":"broken"`,
			want:          `request failed with status 500: {"message":"broken"`,
			serverMessage: `{"message":"broken"`,
		},
		{
			name:          "HTML retains raw body",
			code:          502,
			body:          "<html><body>bad gateway</body></html>",
			want:          "request failed with status 502: <html><body>bad gateway</body></html>",
			serverMessage: "<html><body>bad gateway</body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := providers.FormatError(tt.code, []byte(tt.body))
			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())

			var statusErr *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, tt.serverMessage, statusErr.ServerMessage)
			assert.Equal(t, tt.traceID, statusErr.TraceID)
			assert.Empty(t, statusErr.ContentType, "FormatError has no response headers")
			// The status and server metadata travel out-of-band; transport text
			// stays byte-for-byte compatible.
			var carrier interface{ HTTPStatusCode() int }
			require.ErrorAs(t, err, &carrier, "every FormatError form must carry its status")
			assert.Equal(t, tt.code, carrier.HTTPStatusCode())
			require.NoError(t, errors.Unwrap(err),
				"FormatError never wrapped anything and must not start: converters walk these chains")

			// Implementing APIServiceName and APIUserMessage would satisfy
			// cmd/gcx/fail's serviceAPIError and shadow specialized converters.
			var serviceShaped interface {
				error
				HTTPStatusCode() int
				APIServiceName() string
				APIUserMessage() string
			}
			assert.NotErrorAs(t, err, &serviceShaped,
				"the provider error must implement only the status accessor")
		})
	}
}

// The body-read-failure path was the one HandleErrorResponse form without a
// test, and the one that wraps: the known status must be retained while the
// reader error stays reachable through Unwrap, as the previous %w exposed it.
func TestHandleErrorResponseReadFailureCarriesStatusAndCause(t *testing.T) {
	readErr := errors.New("boom")
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:       io.NopCloser(&failingReader{err: readErr}),
	}

	err := providers.HandleErrorResponse(resp)
	require.Error(t, err)
	assert.Equal(t, "request failed with status 502 (could not read body: boom)", err.Error())
	require.ErrorIs(t, err, readErr, "the reader error must stay in the unwrap chain")

	var statusErr *gcxerrors.HTTPStatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, "text/html; charset=utf-8", statusErr.ContentType)

	var carrier interface{ HTTPStatusCode() int }
	require.ErrorAs(t, err, &carrier)
	assert.Equal(t, http.StatusBadGateway, carrier.HTTPStatusCode(),
		"a body-read failure must not lose the status the response already carried")
}

type failingReader struct{ err error }

func (r *failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestConfirmDestructive_NonInteractiveEOF(t *testing.T) {
	// Pin the env so the interactive prompt path always runs: agent sessions
	// (CLAUDECODE) would otherwise take the agent-mode error path, and
	// GCX_AUTO_APPROVE would bypass the prompt entirely.
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_AUTO_APPROVE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	// Empty stdin (no newline): the read fails with EOF and the error must
	// tell the user how to proceed rather than leaking a bare read error.
	var out strings.Builder
	ok, err := providers.ConfirmDestructive(strings.NewReader(""), &out, false, "Delete it?")
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "use --force")
}

func TestHandleErrorResponsePreservesContentType(t *testing.T) {
	for _, body := range []string{"", "<html>login</html>", `{"message":"denied"}`} {
		t.Run(body, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}
			err := providers.HandleErrorResponse(resp)
			require.EqualError(t, err, providers.FormatError(http.StatusForbidden, []byte(body)).Error())
			var statusErr *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, "text/html; charset=utf-8", statusErr.ContentType)
		})
	}
}
