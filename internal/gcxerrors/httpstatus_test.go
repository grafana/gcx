package gcxerrors_test

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPStatusErrorContract(t *testing.T) {
	plain := &gcxerrors.HTTPStatusError{
		Status:        http.StatusBadGateway,
		Message:       "request failed with status 502",
		ServerMessage: "upstream unavailable",
		TraceID:       "abc123",
	}
	assert.Equal(t, "request failed with status 502", plain.Error(),
		"Message is the whole rendered text, nothing is appended")
	assert.Equal(t, http.StatusBadGateway, plain.HTTPStatusCode())
	require.NoError(t, errors.Unwrap(plain), "a message that never wrapped anything unwraps to nil")

	wrapped := &gcxerrors.HTTPStatusError{Status: http.StatusInternalServerError, Message: "read failed", Cause: io.ErrUnexpectedEOF}
	require.ErrorIs(t, wrapped, io.ErrUnexpectedEOF, "the cause chain must survive for errors.Is")

	var carrier interface{ HTTPStatusCode() int }
	require.ErrorAs(t, error(wrapped), &carrier, "the one-method structural probe must match")
	assert.Equal(t, http.StatusInternalServerError, carrier.HTTPStatusCode())
}

// This type must not satisfy cmd/gcx/fail's three-method serviceAPIError
// interface. Doing so would shadow the specialized provider converters and
// the final HTTPStatusError converter. Data fields must not grow the method set.
func TestHTTPStatusErrorNeverSatisfiesServiceAPIError(t *testing.T) {
	var serviceShaped interface {
		error
		HTTPStatusCode() int
		APIServiceName() string
		APIUserMessage() string
	}
	err := error(&gcxerrors.HTTPStatusError{Status: http.StatusUnauthorized, Message: "request failed with status 401"})
	assert.NotErrorAs(t, err, &serviceShaped,
		"HTTPStatusError must not grow APIServiceName/APIUserMessage; that bypasses specialized converters")
}
