package fail_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/queryerror"
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
	assert.Equal(t, "Some other error", got.Summary)
	assert.Empty(t, got.Details)
	assert.NoError(t, got.Parent)
}

func TestErrorToDetailedError_WrappedErrorUsesOuterSummary(t *testing.T) {
	got := fail.ErrorToDetailedError(fmt.Errorf("failed to create client: %w", errors.New("dial tcp 127.0.0.1: connect: connection refused")))

	require.NotNil(t, got)
	assert.Equal(t, "Failed to create client", got.Summary)
	require.Error(t, got.Parent)
	assert.Equal(t, "dial tcp 127.0.0.1: connect: connection refused", got.Parent.Error())
}

func TestErrorToDetailedError_ColonSeparatedMessageSplitsSummaryAndDetails(t *testing.T) {
	got := fail.ErrorToDetailedError(errors.New("datasource UID is required: use -d flag or set datasources.loki in config"))

	require.NotNil(t, got)
	assert.Equal(t, "Datasource UID is required", got.Summary)
	assert.Equal(t, "use -d flag or set datasources.loki in config", got.Details)
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

func TestErrorToDetailedError_QueryParseError(t *testing.T) {
	err := fmt.Errorf("query failed: %w", queryerror.New(
		"loki",
		"query",
		400,
		"parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING",
		"downstream",
	))

	got := fail.ErrorToDetailedError(err)

	require.NotNil(t, got)
	assert.Equal(t, "Invalid LogQL query", got.Summary)
	assert.Equal(t, "parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING", got.Details)
	require.Len(t, got.Suggestions, 2)
	assert.Equal(t, `Try a quoted selector value, e.g. gcx logs query '{namespace="prod"}'`, got.Suggestions[0])
	assert.Equal(t, "Run 'gcx logs query --help' for usage and examples", got.Suggestions[1])
	assert.Nil(t, got.ExitCode)
}

func TestErrorToDetailedError_QueryAuthFailure(t *testing.T) {
	got := fail.ErrorToDetailedError(queryerror.New("prometheus", "query", 401, "unauthorized", ""))

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed querying Prometheus", got.Summary)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
	assert.Equal(t, []string{
		"Review your Grafana credentials: gcx config view",
		"Re-authenticate if needed: gcx auth login",
	}, got.Suggestions)
}

func TestErrorToDetailedError_DatasourceNotFound(t *testing.T) {
	got := fail.ErrorToDetailedError(fmt.Errorf("failed to get datasource: %w", &datasources.APIError{
		Operation:  "get datasource",
		Identifier: "missing",
		StatusCode: 404,
		Message:    "Datasource not found",
	}))

	require.NotNil(t, got)
	assert.Equal(t, `Datasource "missing" not found`, got.Summary)
	assert.Equal(t, "Datasource not found", got.Details)
	assert.Equal(t, []string{"List available datasources: gcx datasources list"}, got.Suggestions)
}

func TestErrorToDetailedError_WrappedDatasourceErrorPreservesUID(t *testing.T) {
	// Wrapper pattern from internal/datasources/query/resolve.go:
	//     fmt.Errorf("failed to get datasource %q: %w", uid, err)
	// The UID identifies which datasource failed and must survive the
	// generic-wrapper filter so users can tell them apart in flows that
	// query multiple datasources.
	err := fmt.Errorf("failed to get datasource %q: %w", "my-prom-uid", &datasources.APIError{
		Operation:  "get datasource",
		Identifier: "my-prom-uid",
		StatusCode: 404,
		Message:    "Datasource not found",
	})

	got := fail.ErrorToDetailedError(err)

	require.NotNil(t, got)
	assert.Equal(t, `Datasource "my-prom-uid" not found`, got.Summary)
	assert.Contains(t, got.Details, `failed to get datasource "my-prom-uid"`,
		"UID-bearing wrapper prefix must be preserved so users can identify which datasource failed")
	assert.Contains(t, got.Details, "Datasource not found")
	assert.Equal(t, []string{"List available datasources: gcx datasources list"}, got.Suggestions)
}

func TestErrorToDetailedError_WrappedDatasourceErrorPreservesOuterGuidance(t *testing.T) {
	err := fmt.Errorf(
		"SM metrics datasource %q not found in Grafana: %w; use --datasource-uid or set default-prometheus-datasource in config",
		"sm-prom",
		&datasources.APIError{
			Operation:  "get datasource",
			Identifier: "sm-prom",
			StatusCode: 404,
			Message:    "Datasource not found",
		},
	)

	got := fail.ErrorToDetailedError(err)

	require.NotNil(t, got)
	assert.Equal(t, `Datasource "sm-prom" not found`, got.Summary)
	assert.Contains(t, got.Details, `SM metrics datasource "sm-prom" not found in Grafana`)
	assert.Contains(t, got.Details, "use --datasource-uid or set default-prometheus-datasource in config")
	assert.Contains(t, got.Details, "Datasource not found")
	assert.Equal(t, []string{"List available datasources: gcx datasources list"}, got.Suggestions)
}

func TestErrorToDetailedError_QueryNotFoundUsesResourceSummary(t *testing.T) {
	got := fail.ErrorToDetailedError(queryerror.New("tempo", "get trace", 404, "trace not found", ""))

	require.NotNil(t, got)
	assert.Equal(t, "Trace not found", got.Summary)
	assert.Equal(t, "trace not found", got.Details)
}

func TestErrorToDetailedError_GenericServiceAPIAuthFailure(t *testing.T) {
	got := fail.ErrorToDetailedError(fakeServiceAPIError{statusCode: 401, service: "Adaptive Logs", message: "invalid API token"})

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed querying Adaptive Logs", got.Summary)
	assert.Equal(t, "invalid API token", got.Details)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
}

func TestErrorToDetailedError_AdaptiveLogsScopeSuggestion(t *testing.T) {
	got := fail.ErrorToDetailedError(fakeServiceAPIError{
		statusCode: 401,
		service:    "Adaptive Logs",
		message:    "authentication error: invalid scope requested",
	})

	require.NotNil(t, got)
	assert.Equal(t, "Adaptive Logs: permission denied", got.Summary)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
	require.Len(t, got.Suggestions, 1)
	assert.Contains(t, got.Suggestions[0], "adaptive-logs:admin")
}

func TestErrorToDetailedError_WrappedServiceAPIErrorPreservesOuterContext(t *testing.T) {
	err := fmt.Errorf("kg: get rule %q: %w", "prod-errors", fakeServiceAPIError{
		statusCode: 404,
		service:    "Knowledge Graph",
		message:    "rule not found",
	})

	got := fail.ErrorToDetailedError(err)

	require.NotNil(t, got)
	assert.Equal(t, "Knowledge Graph API resource not found", got.Summary)
	assert.Contains(t, got.Details, `kg: get rule "prod-errors"`)
	assert.Contains(t, got.Details, "rule not found")
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

func TestErrorToDetailedError_CloudStackLookupForbidden(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantMatch   bool
		wantSummary string
	}{
		{
			name:        "k6 stack info 403 suggests stacks:read scope",
			err:         errors.New("k6: load cloud config: failed to get stack info for \"mystack\": status 403: forbidden"),
			wantMatch:   true,
			wantSummary: "Cloud stack lookup: permission denied",
		},
		{
			name:        "faro stack info 403 also matches",
			err:         errors.New("cloud config required for sourcemap upload: failed to get stack info for \"mystack\": status 403: forbidden"),
			wantMatch:   true,
			wantSummary: "Cloud stack lookup: permission denied",
		},
		{
			name:      "stack info 404 is not matched",
			err:       errors.New("k6: load cloud config: failed to get stack info for \"mystack\": status 404: not found"),
			wantMatch: false,
		},
		{
			name:      "403 without stack info is not matched",
			err:       errors.New("k6: list projects: status 403: forbidden"),
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)

			if !tc.wantMatch {
				assert.Equal(t, "Unexpected error", got.Summary)
				return
			}

			assert.Equal(t, tc.wantSummary, got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], "stacks:read")
		})
	}
}

func TestErrorToDetailedError_FleetScopeError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantScope string
	}{
		{
			name:      "list pipelines invalid scope suggests fleet-management:read",
			err:       errors.New(`fleet: list pipelines: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:read",
		},
		{
			name:      "list collectors invalid scope suggests fleet-management:read",
			err:       errors.New(`fleet: list collectors: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:read",
		},
		{
			name:      "get pipeline invalid scope suggests fleet-management:read",
			err:       errors.New(`fleet: get pipeline abc123: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:read",
		},
		{
			name:      "create pipeline invalid scope suggests fleet-management:write",
			err:       errors.New(`fleet: create pipeline: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:write",
		},
		{
			name:      "update pipeline invalid scope suggests fleet-management:write",
			err:       errors.New(`fleet: update pipeline abc123: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:write",
		},
		{
			name:      "create collector invalid scope suggests fleet-management:write",
			err:       errors.New(`fleet: create collector: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:write",
		},
		{
			name:      "update collector invalid scope suggests fleet-management:write",
			err:       errors.New(`fleet: update collector abc123: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:write",
		},
		{
			name:      "delete pipeline invalid scope suggests fleet-management:write",
			err:       errors.New(`fleet: delete pipeline abc123: status 401: {"status":"error","error":"authentication error: invalid scope requested"}`),
			wantScope: "fleet-management:write",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)

			if tc.wantScope == "" {
				assert.Equal(t, "Unexpected error", got.Summary)
				return
			}

			assert.Equal(t, "Fleet Management: permission denied", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], tc.wantScope)
		})
	}
}

func TestErrorToDetailedError_StacksReadAdaptiveContext(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantScope string
	}{
		{
			name:      "logs signal suggests adaptive-logs:admin",
			err:       errors.New(`adaptive-logs: failed to load cloud config for token: failed to get stack info for "mystack": gcom client: unexpected status 403 Forbidden`),
			wantScope: "adaptive-logs:admin",
		},
		{
			name:      "metrics signal suggests adaptive-metrics-rules:read",
			err:       errors.New(`adaptive-metrics: failed to load cloud config for token: failed to get stack info for "mystack": gcom client: unexpected status 403 Forbidden`),
			wantScope: "adaptive-metrics-rules:read",
		},
		{
			name:      "traces signal suggests adaptive-traces:admin",
			err:       errors.New(`adaptive-traces: failed to load cloud config for token: failed to get stack info for "mystack": gcom client: unexpected status 403 Forbidden`),
			wantScope: "adaptive-traces:admin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)
			require.NotNil(t, got)
			assert.Equal(t, "Cloud stack lookup: permission denied", got.Summary)
			require.Len(t, got.Suggestions, 2)
			assert.Contains(t, got.Suggestions[0], "stacks:read")
			assert.Contains(t, got.Suggestions[1], tc.wantScope)
		})
	}
}

func TestErrorToDetailedError_AdaptiveMetricsScopeError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantScope string
	}{
		{"list rules", errors.New(`metrics: list rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:read"},
		{"get rule", errors.New(`metrics: get rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:read"},
		{"list recommended rules", errors.New(`metrics: list recommended rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:read"},
		{"create rule", errors.New(`metrics: create rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"update rule", errors.New(`metrics: update rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"sync rules", errors.New(`metrics: sync rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"validate rules", errors.New(`metrics: validate rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"delete rule", errors.New(`metrics: delete rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:delete"},
		{"list recommendations", errors.New(`metrics: list recommendations: status 401: authentication error: invalid scope requested`), "adaptive-metrics-recommendations:read"},
		{"list segments", errors.New(`metrics: list segments: status 401: authentication error: invalid scope requested`), "adaptive-metrics-segments:read"},
		{"create segment", errors.New(`metrics: create segment: status 401: authentication error: invalid scope requested`), "adaptive-metrics-segments:write"},
		{"delete segment", errors.New(`metrics: delete segment: status 401: authentication error: invalid scope requested`), "adaptive-metrics-segments:delete"},
		{"list exemptions", errors.New(`metrics: list exemptions: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:read"},
		{"list segmented exemptions", errors.New(`metrics: list segmented exemptions: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:read"},
		{"get exemption", errors.New(`metrics: get exemption: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:read"},
		{"create exemption", errors.New(`metrics: create exemption: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:write"},
		{"delete exemption", errors.New(`metrics: delete exemption: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:delete"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)
			assert.Equal(t, "Adaptive Metrics: permission denied", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], tc.wantScope)
		})
	}
}

func TestErrorToDetailedError_AdaptiveTracesScopeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"list policies", errors.New(`traces: list policies: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"get policy", errors.New(`traces: get policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"create policy", errors.New(`traces: create policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"update policy", errors.New(`traces: update policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"delete policy", errors.New(`traces: delete policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"list recommendations", errors.New(`traces: list recommendations: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"apply recommendation", errors.New(`traces: apply recommendation: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"dismiss recommendation", errors.New(`traces: dismiss recommendation: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)
			assert.Equal(t, "Adaptive Traces: permission denied", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, fail.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], "adaptive-traces:admin")
		})
	}
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

type fakeServiceAPIError struct {
	statusCode int
	service    string
	message    string
}

func (e fakeServiceAPIError) Error() string {
	return e.message
}

func (e fakeServiceAPIError) HTTPStatusCode() int {
	return e.statusCode
}

func (e fakeServiceAPIError) APIServiceName() string {
	return e.service
}

func (e fakeServiceAPIError) APIUserMessage() string {
	return e.message
}
