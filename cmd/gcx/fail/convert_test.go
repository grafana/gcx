package fail_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/grafana"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestErrorToDetailedError_ContextCanceled(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantExitCode int
	}{
		{
			name:         "bare context.Canceled returns ExitCancelled",
			err:          context.Canceled,
			wantExitCode: fail.ExitCancelled,
		},
		{
			name:         "wrapped context.Canceled returns ExitCancelled",
			err:          fmt.Errorf("operation failed: %w", context.Canceled),
			wantExitCode: fail.ExitCancelled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)

			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, tc.wantExitCode, *got.ExitCode)
		})
	}
}

func TestErrorToDetailedError_NonCanceledError(t *testing.T) {
	got := fail.ErrorToDetailedError(errors.New("some other error"))

	require.NotNil(t, got)
	assert.Nil(t, got.ExitCode, "non-canceled errors should have nil ExitCode")
	assert.Equal(t, "Unexpected error", got.Summary)
}

func TestErrorToDetailedError_AuthExitCode(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantExitCode int
	}{
		{
			name: "401 Unauthorized returns ExitAuthFailure",
			err: &k8sapi.StatusError{
				ErrStatus: metav1.Status{
					Status:  metav1.StatusFailure,
					Code:    401,
					Reason:  metav1.StatusReasonUnauthorized,
					Message: "Unauthorized",
				},
			},
			wantExitCode: fail.ExitAuthFailure,
		},
		{
			name: "403 Forbidden returns ExitAuthFailure",
			err: &k8sapi.StatusError{
				ErrStatus: metav1.Status{
					Status:  metav1.StatusFailure,
					Code:    403,
					Reason:  metav1.StatusReasonForbidden,
					Message: "Forbidden",
				},
			},
			wantExitCode: fail.ExitAuthFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)

			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode, "ExitCode should be set for auth errors")
			assert.Equal(t, tc.wantExitCode, *got.ExitCode)
		})
	}
}

func TestErrorToDetailedError_VersionIncompatible(t *testing.T) {
	v, err := semver.NewVersion("11.5.0")
	require.NoError(t, err)

	got := fail.ErrorToDetailedError(&grafana.VersionIncompatibleError{Version: v})

	require.NotNil(t, got)
	require.NotNil(t, got.ExitCode, "ExitCode should be set for version incompatibility")
	assert.Equal(t, fail.ExitVersionIncompatible, *got.ExitCode)
}

func TestErrorToDetailedError_ConverterOrdering(t *testing.T) {
	// A context.Canceled wrapping a 401 error should be classified as
	// cancelled (exit 5), not as auth failure (exit 3), because the
	// cancellation converter runs first in the chain.
	unauthorizedErr := &k8sapi.StatusError{
		ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    401,
			Reason:  metav1.StatusReasonUnauthorized,
			Message: "Unauthorized",
		},
	}
	wrappedErr := fmt.Errorf("request failed: %w: %w", context.Canceled, unauthorizedErr)

	got := fail.ErrorToDetailedError(wrappedErr)

	require.NotNil(t, got)
	require.NotNil(t, got.ExitCode, "ExitCode should be set")
	assert.Equal(t, fail.ExitCancelled, *got.ExitCode, "context.Canceled should take precedence over auth errors")
}

func TestErrorToDetailedError_UsageErrorIncludesExpectedSyntax(t *testing.T) {
	rootCmd := &cobra.Command{Use: "gcx"}
	logsCmd := &cobra.Command{Use: "logs"}
	queryCmd := &cobra.Command{Use: "query [DATASOURCE_UID] EXPR"}
	queryCmd.Flags().Bool("json", false, "")

	rootCmd.AddCommand(logsCmd)
	logsCmd.AddCommand(queryCmd)

	got := fail.ErrorToDetailedError(fail.NewCommandUsageError(queryCmd, "EXPR is required", nil))

	require.NotNil(t, got)
	assert.Equal(t, "Invalid command usage", got.Summary)
	assert.Contains(t, got.Details, "EXPR is required")
	assert.Contains(t, got.Details, "Expected:")
	assert.Contains(t, got.Details, "gcx logs query [DATASOURCE_UID] EXPR [flags]")
	require.Len(t, got.Suggestions, 1)
	assert.Equal(t, "Run 'gcx logs query --help' for full usage and examples", got.Suggestions[0])
}

func TestErrorToDetailedError_UnmarshalErrorSuggestsConfigEdit(t *testing.T) {
	got := fail.ErrorToDetailedError(config.UnmarshalError{
		File: "/home/user/.config/gcx/config.yaml",
		Err:  errors.New(`unknown field "bad-field"`),
	})

	require.NotNil(t, got)
	assert.Equal(t, "Could not parse configuration", got.Summary)
	assert.Contains(t, got.Details, "/home/user/.config/gcx/config.yaml")
	require.Len(t, got.Suggestions, 2)
	assert.Contains(t, got.Suggestions[0], "gcx config edit")
}

func TestErrorToDetailedError_CobraUnknownCommandError(t *testing.T) {
	got := fail.ErrorToDetailedError(errors.New(`unknown command "foo" for "gcx kg"`))

	require.NotNil(t, got)
	assert.Equal(t, "Invalid command usage", got.Summary)
	assert.Equal(t, `unknown command "foo" for "gcx kg"`, got.Details)
	require.Len(t, got.Suggestions, 1)
	assert.Equal(t, "Run 'gcx kg --help' for full usage and examples", got.Suggestions[0])
}

func TestErrorToDetailedError_SMURLNotConfigured(t *testing.T) {
	err := fmt.Errorf("failed to load SM config for checks: %w",
		fmt.Errorf("SM URL not configured: %w", errors.New("no Grafana server configured: grafana config is required")))

	got := fail.ErrorToDetailedError(err)

	require.NotNil(t, got)
	assert.Equal(t, "SM URL not configured", got.Summary)
	assert.Contains(t, got.Details, "SM URL not configured")
	require.Len(t, got.Suggestions, 4)
	assert.Contains(t, got.Suggestions[0], "gcx config set providers.synth.sm-url")
	assert.Contains(t, got.Suggestions[1], "GRAFANA_PROVIDER_SYNTH_SM_URL")
	assert.Contains(t, got.Suggestions[2], "grafana.server")
	assert.Contains(t, got.Suggestions[3], "gcx config view")
}

func TestErrorToDetailedError_SMTokenNotConfigured(t *testing.T) {
	err := fmt.Errorf("failed to load SM config for checks: %w",
		fmt.Errorf("SM token not configured: %w", errors.New("no cloud config: cloud token is required")))

	got := fail.ErrorToDetailedError(err)

	require.NotNil(t, got)
	assert.Equal(t, "SM token not configured", got.Summary)
	assert.Contains(t, got.Details, "SM token not configured")
	require.Len(t, got.Suggestions, 4)
	assert.Contains(t, got.Suggestions[0], "gcx config set providers.synth.sm-token")
	assert.Contains(t, got.Suggestions[1], "GRAFANA_PROVIDER_SYNTH_SM_TOKEN")
	assert.Contains(t, got.Suggestions[2], "cloud.token")
	assert.Contains(t, got.Suggestions[3], "gcx config view")
}
