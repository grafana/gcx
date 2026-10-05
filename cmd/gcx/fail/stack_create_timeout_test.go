package fail_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStackCreationTimeoutRecovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		creation bool
	}{
		{"creation deadline", true}, {"ordinary request deadline", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error = &url.Error{Op: "Post", URL: "https://example.com/api/instances", Err: context.DeadlineExceeded}
			if tc.creation {
				err = &cloud.StackCreationTimeoutError{Slug: "demo", Err: err}
			}
			got := fail.ErrorToDetailedError(fmt.Errorf("failed to create stack: %w", err))
			require.NotNil(t, got)
			if tc.creation {
				assert.Contains(t, got.Details, "may already exist or still be provisioning")
				require.NotNil(t, got.ExitCode)
				assert.Equal(t, gcxerrors.ExitGeneralError, *got.ExitCode)
				assert.Contains(t, got.Suggestions[0], "gcx cloud stacks get demo")
				assert.Contains(t, got.Suggestions[1], "before retrying")
			} else {
				assert.NotContains(t, got.Details, "provisioning")
				assert.Contains(t, got.Suggestions, "Make sure that the API is reachable")
			}
		})
	}
}
