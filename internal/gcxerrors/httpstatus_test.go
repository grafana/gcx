package gcxerrors_test

import (
	"errors"
	"io"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPStatusErrorContract(t *testing.T) {
	plain := &gcxerrors.HTTPStatusError{Status: 502, Message: "request failed with status 502"}
	assert.Equal(t, "request failed with status 502", plain.Error(),
		"Message is the whole rendered text, nothing is appended")
	assert.Equal(t, 502, plain.HTTPStatusCode())
	assert.Nil(t, errors.Unwrap(plain), "a message that never wrapped anything unwraps to nil")

	wrapped := &gcxerrors.HTTPStatusError{Status: 500, Message: "read failed", Cause: io.ErrUnexpectedEOF}
	assert.ErrorIs(t, wrapped, io.ErrUnexpectedEOF, "the cause chain must survive for errors.Is")

	var carrier interface{ HTTPStatusCode() int }
	require.ErrorAs(t, error(wrapped), &carrier, "the one-method structural probe must match")
	assert.Equal(t, 500, carrier.HTTPStatusCode())
}

// The exit-code taxonomy depends on this type NOT satisfying cmd/gcx/fail's
// three-method serviceAPIError interface. If someone adds APIServiceName and
// APIUserMessage, convertServiceAPIErrors starts matching every provider
// error built from this type: 401/403 responses flip from exit 1 to exit 3,
// and the string-matching SM/cloud/stacks/fleet converters behind it are
// shadowed. This test is the tripwire.
func TestHTTPStatusErrorNeverSatisfiesServiceAPIError(t *testing.T) {
	var serviceShaped interface {
		error
		HTTPStatusCode() int
		APIServiceName() string
		APIUserMessage() string
	}
	err := error(&gcxerrors.HTTPStatusError{Status: 401, Message: "request failed with status 401"})
	assert.False(t, errors.As(err, &serviceShaped),
		"HTTPStatusError must not grow APIServiceName/APIUserMessage; that changes exit codes repo-wide")
}
