package fail_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/require"
)

type savedMutationTestError struct{}

func (savedMutationTestError) Error() string {
	return "resource id=example saved; follow-up access denied (HTTP 403)"
}
func (savedMutationTestError) MutationWasSaved() bool { return true }

func TestSavedMutationFailureKeepsPartialStatusWhenReturnedDirectly(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		t.Run(strconv.FormatBool(wrap), func(t *testing.T) {
			var err error = savedMutationTestError{}
			if wrap {
				err = fmt.Errorf("edit resource: %w", err)
			}
			result := fail.ErrorToDetailedError(err)
			require.Equal(t, gcxerrors.SummaryPartialFailure, result.Summary)
			require.NotNil(t, result.ExitCode)
			require.Equal(t, gcxerrors.ExitPartialFailure, *result.ExitCode)
			require.Contains(t, result.Details, "id=example saved")
		})
	}
}
