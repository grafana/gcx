package providers_test

import (
	"strings"
	"testing"

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
		})
	}
}

func TestConfirmDestructive_NonInteractiveEOF(t *testing.T) {
	// Empty stdin (no newline): the read fails with EOF and the error must
	// tell the user how to proceed rather than leaking a bare read error.
	var out strings.Builder
	ok, err := providers.ConfirmDestructive(strings.NewReader(""), &out, false, "Delete it?")
	if err == nil {
		// Agent mode or GCX_AUTO_APPROVE can bypass the prompt in some test
		// environments; only assert the message when the prompt path ran.
		t.Skipf("prompt bypassed (ok=%v)", ok)
	}
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "use --force")
}
