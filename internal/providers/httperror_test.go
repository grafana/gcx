package providers_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatError(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
		want string
	}{
		{
			name: "json message",
			code: 400,
			body: `{"message":"bad request data"}`,
			want: "request failed with status 400: bad request data",
		},
		{
			name: "json message with traceID",
			code: 400,
			body: `{"message":"bad request data","traceID":"abc123"}`,
			want: "request failed with status 400: bad request data (traceID abc123)",
		},
		{
			name: "error field preferred over message",
			code: 500,
			body: `{"error":"boom","message":"ignored"}`,
			want: "request failed with status 500: boom",
		},
		{
			name: "raw body fallback",
			code: 502,
			body: "upstream unavailable",
			want: "request failed with status 502: upstream unavailable",
		},
		{
			name: "empty body",
			code: 503,
			body: "",
			want: "request failed with status 503",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := providers.FormatError(tt.code, []byte(tt.body))
			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())

			// The typed status travels out-of-band; the message stays the whole
			// user-facing contract.
			var carrier interface{ HTTPStatusCode() int }
			require.ErrorAs(t, err, &carrier, "every FormatError form must carry its status")
			assert.Equal(t, tt.code, carrier.HTTPStatusCode())
			require.NoError(t, errors.Unwrap(err),
				"FormatError never wrapped anything and must not start: converters walk these chains")

			// Exit-code tripwire: implementing APIServiceName and APIUserMessage
			// as well would satisfy cmd/gcx/fail's serviceAPIError and flip
			// provider 401/403 call sites from exit 1 to exit 3.
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
		Body:       io.NopCloser(&failingReader{err: readErr}),
	}

	err := providers.HandleErrorResponse(resp)
	require.Error(t, err)
	assert.Equal(t, "request failed with status 502 (could not read body: boom)", err.Error())
	require.ErrorIs(t, err, readErr, "the reader error must stay in the unwrap chain")

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
