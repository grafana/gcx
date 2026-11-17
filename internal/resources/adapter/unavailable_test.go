package adapter_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/require"
)

func TestUnavailablePreservesWrappedError(t *testing.T) {
	cause := errors.New("body read failed")
	original := &gcxerrors.HTTPStatusError{Status: 404, Message: "original error text\n", Cause: cause}
	err := adapter.Unavailable(original)
	require.EqualError(t, err, original.Error())
	require.ErrorIs(t, err, adapter.ErrUnavailable)
	require.ErrorIs(t, err, original)
	require.ErrorIs(t, err, cause)
	var statusErr *gcxerrors.HTTPStatusError
	require.ErrorAs(t, err, &statusErr)
	require.Same(t, original, statusErr)
	require.Same(t, original, errors.Unwrap(err))
}

func TestUnavailableNil(t *testing.T) {
	require.NoError(t, adapter.Unavailable(nil))
}
