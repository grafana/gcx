package gcxerrors_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlreadyReportedExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		ok   bool
	}{
		{name: "typed", err: fmt.Errorf("check failed: %w", gcxerrors.NewAlreadyReportedError(gcxerrors.ExitVersionIncompatible)), want: gcxerrors.ExitVersionIncompatible, ok: true},
		{name: "bare sentinel", err: gcxerrors.ErrAlreadyReported, want: gcxerrors.ExitGeneralError, ok: true},
		{name: "invalid code", err: gcxerrors.NewAlreadyReportedError(0), want: gcxerrors.ExitGeneralError, ok: true},
		{name: "unrelated", err: errors.New("other failure"), want: gcxerrors.ExitSuccess, ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := gcxerrors.AlreadyReportedExitCode(tc.err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				require.ErrorIs(t, tc.err, gcxerrors.ErrAlreadyReported)
			}
		})
	}
}
