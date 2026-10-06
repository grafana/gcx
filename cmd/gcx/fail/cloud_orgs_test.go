package fail_test

import (
	"fmt"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudOrgsErrorConversionScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		auth bool
	}{
		{"no user", fmt.Errorf("failed to list cloud organisations: %w", cloud.ErrUserOAuthRequired), true},
		{"unrelated sentinel", cloud.ErrUserOAuthRequired, false},
		{"server failure", fmt.Errorf("failed to list cloud organisations: %w", &cloud.GCOMHTTPError{Status: 500}), false},
		{"unrelated operation", fmt.Errorf("other operation: %w", &cloud.GCOMHTTPError{Status: 403}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := fail.ErrorToDetailedError(tc.err)
			require.NotNil(t, result)
			if tc.auth {
				require.NotNil(t, result.ExitCode)
				assert.Equal(t, gcxerrors.ExitAuthFailure, *result.ExitCode)
			} else {
				assert.NotEqual(t, "Authorization failed", result.Summary)
			}
		})
	}
}
