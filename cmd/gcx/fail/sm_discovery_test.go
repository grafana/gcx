package fail_test

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
)

func TestSMDiscoveryCauseClassification(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		summary string
		exit    int
	}{
		{"missing cloud", errors.New("no cloud config: context has no cloud auth"), gcxerrors.SummaryAuthenticationFailed, 3},
		{"missing stack", errors.New("no cloud config: cloud stack is not configured"), gcxerrors.SummaryAuthenticationFailed, 3},
		{"register401", fmt.Errorf("register/install: %w", providers.FormatError(401, []byte(`{"message":"denied"}`))), gcxerrors.SummaryAuthorizationFailed, 3},
		{"register403", fmt.Errorf("register/install: %w", providers.FormatError(403, []byte(`{"message":"denied"}`))), gcxerrors.SummaryAuthorizationFailed, 3},
		{"register503", fmt.Errorf("register/install: %w", providers.FormatError(503, []byte(`{"message":"unavailable"}`))), gcxerrors.SummaryAPIError, 1},
		{"register400", fmt.Errorf("register/install: %w", providers.FormatError(400, []byte(`{"message":"bad request"}`))), gcxerrors.SummaryAPIError, 1},
		{"gcom503", fmt.Errorf("no cloud config: %w", &cloud.GCOMHTTPError{Status: 503, Message: "unavailable"}), gcxerrors.SummaryAPIError, 1},
		{"gcom unreachable", fmt.Errorf("no cloud config: %w", &url.Error{Op: "Get", URL: "http://127.0.0.1:1", Err: errors.New("connection refused")}), gcxerrors.SummaryNetworkError, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, fmt.Errorf("SM token not configured: %w", tc.cause))
			assert.Equal(t, tc.summary, got.Summary)
			actual := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				actual = *got.ExitCode
			}
			assert.Equal(t, tc.exit, actual)
			text := got.Details
			if got.Parent != nil {
				text += got.Parent.Error()
			}
			assert.Contains(t, text, "auto-discovery")
		})
	}
}
