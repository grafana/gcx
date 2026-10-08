package fail_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorToDetailedError_AdapterNotFound(t *testing.T) {
	for _, err := range []error{
		adapter.ErrNotFound,
		fmt.Errorf("incidents: get missing-id: %w", fmt.Errorf("incident: %w", adapter.ErrNotFound)),
		fmt.Errorf("pipeline %q: %w", "absent-name", adapter.ErrNotFound),
	} {
		t.Run(err.Error(), func(t *testing.T) {
			got := fail.ErrorToDetailedError(err)
			require.NotNil(t, got)
			assert.Equal(t, gcxerrors.SummaryResourceNotFound, got.Summary)
			assert.Equal(t, err.Error(), got.Details)
			require.NoError(t, got.Parent, "details already contain the complete cause")
			assert.Nil(t, got.ExitCode, "resource misses use the default exit code 1")
		})
	}
}

func TestErrorToDetailedError_AdapterNotFoundPreservesSpecificClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		summary string
		exit    int
	}{
		{"canceled", context.Canceled, gcxerrors.SummaryOperationCancelled, gcxerrors.ExitCancelled},
		{"Fleet permission failure", &fleet.HTTPError{Status: http.StatusForbidden, Path: "/GetPipeline", Body: `{"message":"denied"}`}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(errors.Join(tc.cause, adapter.ErrNotFound))
			require.NotNil(t, got)
			assert.Equal(t, tc.summary, got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, tc.exit, *got.ExitCode)
		})
	}
}

func TestErrorToDetailedError_NotFoundTextIsNotASentinel(t *testing.T) {
	got := fail.ErrorToDetailedError(errors.New("incident: not found"))
	assert.Equal(t, gcxerrors.SummaryUnexpectedError, got.Summary)
}

func TestErrorToDetailedError_UnsupportedResourceRecovery(t *testing.T) {
	cause := &resources.UnsupportedResourceError{Selector: "dashboardz", Reason: "the server does not support this resource"}
	err := fmt.Errorf("get resources: %w", cause)
	got := fail.ErrorToDetailedError(err)
	require.NotNil(t, got)
	assert.Equal(t, gcxerrors.SummaryEndpointNotAvailable, got.Summary)
	assert.Nil(t, got.ExitCode, "unavailable resources use the default exit code 1")
	assert.Contains(t, got.Suggestions, "List the resource types this server serves: gcx resources list-types")
	assert.ErrorIs(t, got.Parent, cause)
}
