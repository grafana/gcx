package fail_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthFailureRemediation(t *testing.T) {
	converters := []struct {
		name string
		err  func(int) error
	}{
		{"query", func(status int) error { return queryerror.New("loki", "query", status, "denied", "") }},
		{"datasource", func(status int) error {
			return &datasources.APIError{StatusCode: status, Operation: "list datasources"}
		}},
		{"service", func(status int) error {
			return fakeServiceAPIError{statusCode: status, service: "Knowledge Graph", message: "denied"}
		}},
	}
	for _, converter := range converters {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			t.Run(fmt.Sprintf("%s/%d", converter.name, status), func(t *testing.T) {
				got := toDetailedError(t, fmt.Errorf("caller operation: %w", converter.err(status)))
				require.NotNil(t, got.ExitCode)
				assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
				assert.Contains(t, got.Details, "caller operation")
				suggestions := strings.Join(got.Suggestions, " ")
				if status == http.StatusForbidden {
					assert.Equal(t, gcxerrors.SummaryAuthorizationFailed, got.Summary)
					assert.Contains(t, suggestions, "roles")
					assert.Contains(t, suggestions, "scopes")
					assert.Contains(t, suggestions, "gcx setup status")
					assert.NotContains(t, suggestions, "gcx login")
				} else {
					assert.Equal(t, gcxerrors.SummaryAuthenticationFailed, got.Summary)
					assert.Contains(t, suggestions, "gcx login")
				}
			})
		}
	}
}

func TestSMMissingDiscoveryPrerequisites(t *testing.T) {
	for _, cause := range []string{
		"context has no cloud auth: run gcx cloud login",
		`cloud entry "example" has no token`,
		"cloud stack is not configured: set the slug",
	} {
		t.Run(cause, func(t *testing.T) {
			err := fmt.Errorf("checks list: SM token not configured: no cloud config: %w", errors.New(cause))
			got := toDetailedError(t, err)
			assert.Equal(t, gcxerrors.SummaryAuthenticationFailed, got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			assert.Contains(t, got.Details, cause)
			assert.Contains(t, strings.Join(got.Suggestions, " "), "gcx cloud login")
		})
	}
}
