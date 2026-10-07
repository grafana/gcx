package fail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/fleet"
	gcxerrors "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/login"
	cmdoutput "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
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
			wantExitCode: gcxerrors.ExitCancelled,
		},
		{
			name:         "wrapped context.Canceled returns ExitCancelled",
			err:          fmt.Errorf("operation failed: %w", context.Canceled),
			wantExitCode: gcxerrors.ExitCancelled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)

			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, tc.wantExitCode, *got.ExitCode)
		})
	}
}

// TestErrorToDetailedError_Fallback pins the fallback contract: an error no
// typed converter claims gets the Unexpected error summary, and its full,
// trimmed message goes to the details, whatever the message looks like.
func TestErrorToDetailedError_Fallback(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantDetails string
	}{
		{
			name:        "plain message",
			err:         errors.New("some other error"),
			wantDetails: "some other error",
		},
		{
			name:        "wrapped error keeps the wrapper and the cause",
			err:         fmt.Errorf("failed to create client: %w", errors.New("dial tcp 127.0.0.1: connect: connection refused")),
			wantDetails: "failed to create client: dial tcp 127.0.0.1: connect: connection refused",
		},
		{
			name:        "colon-separated message is not split into a summary",
			err:         errors.New("datasource UID is required: use -d flag or set datasources.loki in config"),
			wantDetails: "datasource UID is required: use -d flag or set datasources.loki in config",
		},
		{
			name:        "surrounding whitespace is trimmed",
			err:         errors.New("  multi-line failure\nsecond line\n"),
			wantDetails: "multi-line failure\nsecond line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)

			require.NotNil(t, got)
			assert.Equal(t, gcxerrors.SummaryUnexpectedError, got.Summary)
			assert.Equal(t, tc.wantDetails, got.Details)
			assert.Nil(t, got.ExitCode, "the fallback keeps the default exit code")
			require.NoError(t, got.Parent, "Parent would repeat the details")
			assert.Equal(t, 1, strings.Count(got.Error(), strings.SplitN(tc.wantDetails, "\n", 2)[0]), "text output shows the message once")
		})
	}
}

func TestErrorToDetailedError_ContextNotFoundListsAvailable(t *testing.T) {
	got := toDetailedError(t, config.ContextNotFound("ops", []string{"auth", "default", "dev"}))

	require.NotNil(t, got)
	assert.Equal(t, "Invalid configuration", got.Summary)
	require.NotEmpty(t, got.Suggestions)
	// The first suggestion is a runnable command (docs/design/errors.md 4.2).
	assert.Equal(t,
		"Use one of the configured contexts (auth, default, dev), for example: gcx config use-context auth",
		got.Suggestions[0])
	assert.Contains(t, got.Suggestions, "Check for typos in the context name")
	assert.Contains(t, got.Suggestions, "Review your configuration: gcx config view")
}

func TestErrorToDetailedError_ContextNotFoundCapsList(t *testing.T) {
	got := toDetailedError(t, config.ContextNotFound(
		"ops", []string{"a", "b", "c", "d", "e", "f", "g"}))

	require.NotNil(t, got)
	require.NotEmpty(t, got.Suggestions)
	assert.Equal(t,
		"Use one of the configured contexts (a, b, c, d, e, +2 more), for example: gcx config use-context a",
		got.Suggestions[0], "the inline list is capped so a large config does not emit a huge line")
}

func TestErrorToDetailedError_ContextNotFoundWithoutAvailable(t *testing.T) {
	got := toDetailedError(t, config.ContextNotFound("ops", nil))

	require.NotNil(t, got)
	require.Len(t, got.Suggestions, 2)
	for _, s := range got.Suggestions {
		assert.NotContains(t, s, "configured contexts")
	}
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
			wantExitCode: gcxerrors.ExitAuthFailure,
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
			wantExitCode: gcxerrors.ExitAuthFailure,
		},
		{
			name: "403 from the dynamic client (dynamic.APIError) returns ExitAuthFailure",
			err: fmt.Errorf("list routing trees: %w", dynamic.ParseStatusError(&k8sapi.StatusError{
				ErrStatus: metav1.Status{
					Status:  metav1.StatusFailure,
					Code:    403,
					Reason:  metav1.StatusReasonForbidden,
					Message: "Forbidden",
				},
			})),
			wantExitCode: gcxerrors.ExitAuthFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)

			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode, "ExitCode should be set for auth errors")
			assert.Equal(t, tc.wantExitCode, *got.ExitCode)
		})
	}
}

func TestErrorToDetailedError_VersionIncompatible(t *testing.T) {
	v, err := semver.NewVersion("11.5.0")
	require.NoError(t, err)

	got := toDetailedError(t, &grafana.VersionIncompatibleError{Version: v})

	require.NotNil(t, got)
	assert.Equal(t, "Unsupported Grafana version", got.Summary)
	assert.Equal(t, "Grafana version 11.5.0 is not supported: gcx requires Grafana 12.0.0 or later", got.Details)
	require.NotNil(t, got.ExitCode, "ExitCode should be set for version incompatibility")
	assert.Equal(t, gcxerrors.ExitVersionIncompatible, *got.ExitCode)
	assert.Equal(t, docs.GrafanaInstallation, got.DocsLink)
}

func TestErrorToDetailedError_QueryParseError(t *testing.T) {
	err := fmt.Errorf("query failed: %w", queryerror.New(
		"loki",
		"query",
		400,
		"parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING",
		"downstream",
	))

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Invalid query", got.Summary)
	assert.Equal(t, "Invalid LogQL query (HTTP 400)\n\nparse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING", got.Details)
	require.Len(t, got.Suggestions, 2)
	assert.Equal(t, `Try a quoted selector value, e.g. gcx logs query '{namespace="prod"}'`, got.Suggestions[0])
	assert.Equal(t, "Run 'gcx logs query --help' for usage and examples", got.Suggestions[1])
	assert.Equal(t, docs.LogQL, got.DocsLink, "parse errors should point at the query-language docs")
	assert.Nil(t, got.ExitCode)
}

func TestErrorToDetailedError_ProfileSeriesQuery(t *testing.T) {
	got := toDetailedError(t, queryerror.New(
		"pyroscope",
		"profile series query",
		400,
		"parse error: expecting string",
		"downstream",
	))

	require.NotNil(t, got)
	assert.Equal(t, "Invalid query", got.Summary)
	assert.True(t, strings.HasPrefix(got.Details, "Invalid Pyroscope selector query (HTTP 400)"), got.Details)
	assert.Contains(t, got.Suggestions, `Try a quoted selector value, e.g. gcx profiles series '{service_name="frontend"}'`)
	assert.Contains(t, got.Suggestions, "Run 'gcx profiles series --help' for usage and examples")
	assert.Equal(t, docs.PyroscopeQueries, got.DocsLink)
}

func TestErrorToDetailedError_QueryAuthFailure(t *testing.T) {
	tests := []struct {
		status      int
		message     string
		wantSummary string
	}{
		{401, "unauthorized", "Authentication failed"},
		{403, "forbidden", "Authorization failed"},
	}
	for _, tc := range tests {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			got := toDetailedError(t, queryerror.New("prometheus", "query", tc.status, tc.message, ""))

			require.NotNil(t, got)
			assert.Equal(t, tc.wantSummary, got.Summary)
			assert.Equal(t, fmt.Sprintf("Prometheus query failed (HTTP %d)\n\n%s", tc.status, tc.message), got.Details)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			if tc.status == http.StatusForbidden {
				assert.Contains(t, got.Suggestions, "Check your Grafana service-account roles and access-policy scopes")
				assert.Contains(t, got.Suggestions, "Check access: gcx setup status")
				assert.NotContains(t, strings.Join(got.Suggestions, " "), "gcx login")
			} else {
				assert.Equal(t, []string{
					"Review your Grafana credentials: gcx config view",
					"Re-authenticate if needed: gcx login",
				}, got.Suggestions)
			}
			assert.Equal(t, docs.ServiceAccounts, got.DocsLink, "auth failures should point at the service-account docs")
		})
	}
}

func TestErrorToDetailedError_SessionExpiredDocsLink(t *testing.T) {
	got := toDetailedError(t, fmt.Errorf("token refresh failed: %w", auth.ErrRefreshTokenExpired))

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed", got.Summary)
	assert.Equal(t, docs.ServiceAccounts, got.DocsLink)
}

// TestErrorToDetailedError_DocsLinksAreMarkdown asserts that every DocsLink
// populated by the converters is a Markdown (.md) URL, so agents never receive
// an HTML doc link from an error.
func TestErrorToDetailedError_DocsLinksAreMarkdown(t *testing.T) {
	cases := []error{
		fmt.Errorf("token refresh failed: %w", auth.ErrRefreshTokenExpired),
		&grafana.VersionIncompatibleError{Version: semver.MustParse("11.5.0")},
		queryerror.New("prometheus", "query", 401, "unauthorized", ""),
		queryerror.New("tempo", "search query", 400, "parse error: unexpected token", "downstream"),
	}
	for _, err := range cases {
		got := toDetailedError(t, err)
		require.NotNil(t, got)
		if got.DocsLink != "" {
			assert.True(t, strings.HasSuffix(got.DocsLink, ".md"),
				"DocsLink %q must end in .md", got.DocsLink)
		}
	}
}

func TestErrorToDetailedError_DatasourceNotFound(t *testing.T) {
	got := toDetailedError(t, fmt.Errorf("failed to get datasource: %w", &datasources.APIError{
		Operation:  "get datasource",
		Identifier: "missing",
		StatusCode: 404,
		Message:    "Datasource not found",
	}))

	require.NotNil(t, got)
	assert.Equal(t, "Resource not found", got.Summary)
	assert.Equal(t, "Datasource \"missing\" not found\n\nDatasource not found", got.Details)
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

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Resource not found", got.Summary)
	assert.True(t, strings.HasPrefix(got.Details, `Datasource "my-prom-uid" not found`), got.Details)
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

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Resource not found", got.Summary)
	assert.True(t, strings.HasPrefix(got.Details, `Datasource "sm-prom" not found`), got.Details)
	assert.Contains(t, got.Details, `SM metrics datasource "sm-prom" not found in Grafana`)
	assert.Contains(t, got.Details, "use --datasource-uid or set default-prometheus-datasource in config")
	assert.Contains(t, got.Details, "Datasource not found")
	assert.Equal(t, []string{"List available datasources: gcx datasources list"}, got.Suggestions)
}

func TestErrorToDetailedError_QueryNotFoundUsesResourceSummary(t *testing.T) {
	got := toDetailedError(t, queryerror.New("tempo", "get trace", 404, "trace not found", ""))

	require.NotNil(t, got)
	assert.Equal(t, "Resource not found", got.Summary)
	assert.Equal(t, "Tempo get trace failed (HTTP 404)\n\ntrace not found", got.Details)
}

func TestErrorToDetailedError_GenericServiceAPIAuthFailure(t *testing.T) {
	got := toDetailedError(t, fakeServiceAPIError{statusCode: 401, service: "Adaptive Logs", message: "invalid API token"})

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed", got.Summary)
	assert.Equal(t, "Adaptive Logs API request failed (HTTP 401)\n\ninvalid API token", got.Details)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
}

func TestErrorToDetailedError_AdaptiveLogsScopeSuggestion(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			got := toDetailedError(t, fmt.Errorf("adaptive-logs: list exemptions: %w", fakeServiceAPIError{
				statusCode: status,
				service:    "Adaptive Logs",
				message:    "authentication error: invalid scope requested",
			}))

			require.NotNil(t, got)
			assert.Equal(t, "Authorization failed", got.Summary, "a scope error is a permission problem regardless of status")
			assert.Contains(t, got.Details, fmt.Sprintf("Adaptive Logs API request failed (HTTP %d)", status))
			assert.Contains(t, got.Details, "adaptive-logs: list exemptions")
			assert.Contains(t, got.Details, "invalid scope requested")
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], "adaptive-logs:admin")

			var rendered bytes.Buffer
			require.NoError(t, got.WriteJSON(&rendered, *got.ExitCode))
			assert.Contains(t, rendered.String(), "invalid scope requested", "agents keep the server message")
		})
	}
}

func TestErrorToDetailedError_WrappedServiceAPIErrorPreservesOuterContext(t *testing.T) {
	err := fmt.Errorf("kg: get rule %q: %w", "prod-errors", fakeServiceAPIError{
		statusCode: 404,
		service:    "Knowledge Graph",
		message:    "rule not found",
	})

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Resource not found", got.Summary)
	assert.True(t, strings.HasPrefix(got.Details, "Knowledge Graph API request failed (HTTP 404)"), got.Details)
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

	got := toDetailedError(t, wrappedErr)

	require.NotNil(t, got)
	require.NotNil(t, got.ExitCode, "ExitCode should be set")
	assert.Equal(t, gcxerrors.ExitCancelled, *got.ExitCode, "context.Canceled should take precedence over auth errors")
}

func TestErrorToDetailedError_UsageErrorIncludesExpectedSyntax(t *testing.T) {
	rootCmd := &cobra.Command{Use: "gcx"}
	logsCmd := &cobra.Command{Use: "logs"}
	queryCmd := &cobra.Command{Use: "query [DATASOURCE_UID] EXPR"}
	queryCmd.Flags().Bool("json", false, "")

	rootCmd.AddCommand(logsCmd)
	logsCmd.AddCommand(queryCmd)

	got := toDetailedError(t, fail.NewCommandUsageError(queryCmd, "EXPR is required", nil))

	require.NotNil(t, got)
	assert.Equal(t, "Invalid command usage", got.Summary)
	assert.Contains(t, got.Details, "EXPR is required")
	assert.Contains(t, got.Details, "Expected:")
	assert.Contains(t, got.Details, "gcx logs query [DATASOURCE_UID] EXPR [flags]")
	require.Len(t, got.Suggestions, 1)
	assert.Equal(t, "Run 'gcx logs query --help' for full usage and examples", got.Suggestions[0])
}

func TestErrorToDetailedError_UnmarshalErrorSuggestsConfigEdit(t *testing.T) {
	got := toDetailedError(t, config.UnmarshalError{
		File: "/home/user/.config/gcx/config.yaml",
		Err:  errors.New(`unknown field "bad-field"`),
	})

	require.NotNil(t, got)
	assert.Equal(t, "Invalid configuration", got.Summary)
	assert.Equal(t, "Could not parse configuration in '/home/user/.config/gcx/config.yaml'.", got.Details)
	require.Len(t, got.Suggestions, 2)
	assert.Contains(t, got.Suggestions[0], "gcx config edit")
}

func TestErrorToDetailedError_CobraUnknownCommandError(t *testing.T) {
	got := toDetailedError(t, errors.New(`unknown command "foo" for "gcx kg"`))

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
			wantSummary: "Authorization failed",
		},
		{
			name:        "faro stack info 403 also matches",
			err:         errors.New("cloud config required for sourcemap upload: failed to get stack info for \"mystack\": status 403: forbidden"),
			wantMatch:   true,
			wantSummary: "Authorization failed",
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
			got := toDetailedError(t, tc.err)

			if !tc.wantMatch {
				assert.Equal(t, "Unexpected error", got.Summary)
				return
			}

			assert.Equal(t, tc.wantSummary, got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], "stacks:read")
			var rendered bytes.Buffer
			require.NoError(t, got.WriteJSON(&rendered, *got.ExitCode))
			assert.Contains(t, rendered.String(), "failed to get stack info for", "the cause stays in the details")
		})
	}
}

func TestErrorToDetailedError_FleetPluginMissing(t *testing.T) {
	// Grafana answers with this body when the collector app plugin is absent or
	// disabled. It arrives as a 404, the same status Fleet Management returns for
	// an absent resource, so the message must not mention a missing resource.
	err := fmt.Errorf("fleet: list pipelines: %w", &fleet.HTTPError{
		Status: 404,
		Path:   "/pipeline.v1.PipelineService/ListPipelines",
		Body:   `{"message":"plugin route match not found"}`,
	})

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Endpoint not available", got.Summary)
	assert.Contains(t, got.Details, "grafana-collector-app")
	require.NotEmpty(t, got.Suggestions)
	assert.Contains(t, got.Suggestions[0], "gcx setup status")
}

func TestErrorToDetailedError_FleetForbiddenNamesTheAction(t *testing.T) {
	err := fmt.Errorf("fleet: create pipeline: %w", &fleet.HTTPError{
		Status: 403,
		Path:   "/pipeline.v1.PipelineService/CreatePipeline",
		Body:   `{"message":"forbidden"}`,
	})

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Authorization failed", got.Summary)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
	suggestions := strings.Join(got.Suggestions, "\n")
	assert.Contains(t, suggestions, fleet.CollectorAppReadAction)
	assert.Contains(t, suggestions, fleet.CollectorAppAdminAction)
	assert.Contains(t, suggestions, "read-only commands")
}

func TestErrorToDetailedError_StacksReadAdaptiveContext(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantSuggestion string
	}{
		{
			name:           "logs signal suggests adaptive-logs:admin",
			err:            errors.New(`adaptive-logs: failed to load cloud config for token: failed to get stack info for "mystack": gcom client: unexpected status 403 Forbidden`),
			wantSuggestion: "adaptive-logs:admin",
		},
		{
			name:           "metrics signal mentions adaptive-metrics-* scope",
			err:            errors.New(`adaptive-metrics: failed to load cloud config for token: failed to get stack info for "mystack": gcom client: unexpected status 403 Forbidden`),
			wantSuggestion: "adaptive-metrics-*",
		},
		{
			name:           "traces signal suggests adaptive-traces:admin",
			err:            errors.New(`adaptive-traces: failed to load cloud config for token: failed to get stack info for "mystack": gcom client: unexpected status 403 Forbidden`),
			wantSuggestion: "adaptive-traces:admin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)
			require.NotNil(t, got)
			assert.Equal(t, "Authorization failed", got.Summary)
			require.Len(t, got.Suggestions, 2)
			assert.Contains(t, got.Suggestions[0], "stacks:read")
			assert.Contains(t, got.Suggestions[1], tc.wantSuggestion)
		})
	}
}

func TestErrorToDetailedError_AdaptiveMetricsScopeError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantScope string
	}{
		{"list rules", errors.New(`adaptive-metrics: list rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:read"},
		{"get rule", errors.New(`adaptive-metrics: get rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:read"},
		{"list recommended rules", errors.New(`adaptive-metrics: list recommended rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:read"},
		{"create rule", errors.New(`adaptive-metrics: create rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"update rule", errors.New(`adaptive-metrics: update rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"sync rules", errors.New(`adaptive-metrics: sync rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"validate rules", errors.New(`adaptive-metrics: validate rules: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:write"},
		{"delete rule", errors.New(`adaptive-metrics: delete rule: status 401: authentication error: invalid scope requested`), "adaptive-metrics-rules:delete"},
		{"list recommendations", errors.New(`adaptive-metrics: list recommendations: status 401: authentication error: invalid scope requested`), "adaptive-metrics-recommendations:read"},
		{"list segments", errors.New(`adaptive-metrics: list segments: status 401: authentication error: invalid scope requested`), "adaptive-metrics-segments:read"},
		{"create segment", errors.New(`adaptive-metrics: create segment: status 401: authentication error: invalid scope requested`), "adaptive-metrics-segments:write"},
		{"delete segment", errors.New(`adaptive-metrics: delete segment: status 401: authentication error: invalid scope requested`), "adaptive-metrics-segments:delete"},
		{"list exemptions", errors.New(`adaptive-metrics: list exemptions: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:read"},
		{"list segmented exemptions", errors.New(`adaptive-metrics: list segmented exemptions: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:read"},
		{"get exemption", errors.New(`adaptive-metrics: get exemption: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:read"},
		{"create exemption", errors.New(`adaptive-metrics: create exemption: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:write"},
		{"delete exemption", errors.New(`adaptive-metrics: delete exemption: status 401: authentication error: invalid scope requested`), "adaptive-metrics-exemptions:delete"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)
			assert.Equal(t, "Authorization failed", got.Summary)
			assert.Contains(t, got.Error(), "adaptive-metrics:", "the service stays in the details")
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
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
		{"list policies", errors.New(`adaptive-traces: list policies: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"get policy", errors.New(`adaptive-traces: get policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"create policy", errors.New(`adaptive-traces: create policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"update policy", errors.New(`adaptive-traces: update policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"delete policy", errors.New(`adaptive-traces: delete policy: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"list recommendations", errors.New(`adaptive-traces: list recommendations: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"apply recommendation", errors.New(`adaptive-traces: apply recommendation: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
		{"dismiss recommendation", errors.New(`adaptive-traces: dismiss recommendation: unexpected status 401: {"status":"error","error":"authentication error: invalid scope requested"}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)
			assert.Equal(t, "Authorization failed", got.Summary)
			assert.Contains(t, got.Error(), "adaptive-traces:", "the service stays in the details")
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 1)
			assert.Contains(t, got.Suggestions[0], "adaptive-traces:admin")
		})
	}
}

func TestErrorToDetailedError_SMURLNotConfigured(t *testing.T) {
	err := fmt.Errorf("failed to load SM config for checks: %w",
		fmt.Errorf("SM URL not configured: %w", errors.New("no Grafana server configured: grafana config is required")))

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Invalid configuration", got.Summary)
	assert.Contains(t, got.Details, "SM URL not configured")
	require.Len(t, got.Suggestions, 4)
	assert.Contains(t, got.Suggestions[0], "gcx config set stacks.<name>.providers.synth.sm-url")
	assert.Contains(t, got.Suggestions[1], "GRAFANA_PROVIDER_SYNTH_SM_URL")
	assert.Contains(t, got.Suggestions[2], "grafana.server")
	assert.Contains(t, got.Suggestions[3], "gcx config view")
}

func TestErrorToDetailedError_SMTokenNotConfigured(t *testing.T) {
	err := fmt.Errorf("failed to load SM config for checks: %w",
		fmt.Errorf("SM token not configured: %w", errors.New("no cloud config: cloud token is required")))

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed", got.Summary)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
	assert.Contains(t, got.Details, "SM token not configured")
	require.Len(t, got.Suggestions, 4)
	assert.Contains(t, got.Suggestions[0], "gcx config set stacks.<name>.providers.synth.sm-token")
	assert.Contains(t, got.Suggestions[1], "GRAFANA_PROVIDER_SYNTH_SM_TOKEN")
	assert.Contains(t, got.Suggestions[2], "gcx cloud login")
	assert.Contains(t, got.Suggestions[3], "gcx config view")
}

func TestErrorToDetailedError_SMTokenRegisterInstallPermissionDenied(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "HTTP 400 from register/install",
			err: fmt.Errorf("failed to load SM config for checks: %w",
				fmt.Errorf("SM token not configured: %w",
					fmt.Errorf("register/install API failed: %w",
						errors.New("SM register/install: request failed with status 400: insufficient permissions")))),
		},
		{
			name: "HTTP 403 from register/install",
			err: fmt.Errorf("failed to load SM config for checks: %w",
				fmt.Errorf("SM token not configured: %w",
					fmt.Errorf("register/install API failed: %w",
						errors.New("SM register/install: request failed with status 403: forbidden")))),
		},
		{
			name: "HTTP 401 from register/install",
			err: fmt.Errorf("failed to load SM config for checks: %w",
				fmt.Errorf("SM token not configured: %w",
					fmt.Errorf("register/install API failed: %w",
						errors.New("SM register/install: request failed with status 401: unauthorized")))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)

			require.NotNil(t, got)
			assert.Equal(t, "Authorization failed", got.Summary)
			assert.Contains(t, got.Details, "SM token not configured")
			assert.Contains(t, got.Details, "register/install")
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			require.Len(t, got.Suggestions, 3)
			assert.Contains(t, got.Suggestions[0], "stacks:read")
			assert.Contains(t, got.Suggestions[0], "metrics:write")
			assert.Contains(t, got.Suggestions[0], "logs:write")
			assert.Contains(t, got.Suggestions[0], "traces:write")
			assert.Contains(t, got.Suggestions[1], "gcx config set stacks.<name>.providers.synth.sm-token")
		})
	}
}

func TestErrorToDetailedError_SMTokenRegisterInstallGeneric400FallsThrough(t *testing.T) {
	err := fmt.Errorf("failed to load SM config for checks: %w",
		fmt.Errorf("SM token not configured: %w",
			fmt.Errorf("register/install API failed: %w",
				errors.New("SM register/install: request failed with status 400: bad request"))))

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "API error", got.Summary)
	assert.Contains(t, got.Details, "SM token not configured")
	assert.Nil(t, got.ExitCode)
}

func TestErrorToDetailedError_CloudTokenNotConfigured(t *testing.T) {
	err := errors.New("context has no cloud auth: run `gcx cloud login`, or set GRAFANA_CLOUD_TOKEN")

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed", got.Summary)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
	assert.Contains(t, got.Details, "context has no cloud auth")
	require.Len(t, got.Suggestions, 2)
	assert.Contains(t, got.Suggestions[0], "gcx cloud login")
	assert.Contains(t, got.Suggestions[1], "GRAFANA_CLOUD_TOKEN")
}

func TestErrorToDetailedError_CloudEntryTokenMissing(t *testing.T) {
	err := errors.New(`cloud entry "grafana-com" has no token: run ` + "`gcx cloud login`" + `, or set cloud.grafana-com.token or GRAFANA_CLOUD_TOKEN`)

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed", got.Summary)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
	assert.Contains(t, got.Details, `cloud entry "grafana-com" has no token`)
}

func TestErrorToDetailedError_CloudStackNotConfigured(t *testing.T) {
	err := errors.New("cloud stack is not configured: set the stack's slug (gcx config set stacks.<name>.slug <slug>) or GRAFANA_CLOUD_STACK env var")

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Invalid configuration", got.Summary)
	assert.Contains(t, got.Details, "cloud stack is not configured")
	require.Len(t, got.Suggestions, 2)
	assert.Contains(t, got.Suggestions[0], "gcx config set stacks.<name>.slug")
	assert.Contains(t, got.Suggestions[1], "GRAFANA_CLOUD_STACK")
}

func TestErrorToDetailedError_LoginGCOMStack403(t *testing.T) {
	cause := &cloud.GCOMHTTPError{Status: 403, Body: "forbidden"}
	err := &login.GCOMStackError{Slug: "mystack", Status: 403, Cause: cause}

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Authorization failed", got.Summary)
	assert.Equal(t, `Grafana Cloud stack lookup denied: GCOM returned 403 for stack "mystack"`, got.Details)
	require.NotNil(t, got.ExitCode, "403 should map to ExitAuthFailure")
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)

	require.NotEmpty(t, got.Suggestions)
	joined := strings.Join(got.Suggestions, "\n")
	assert.Contains(t, joined, "stacks:read", "must mention the missing CAP scope")
}

func TestErrorToDetailedError_LoginGCOMStack401(t *testing.T) {
	cause := &cloud.GCOMHTTPError{Status: 401, Body: "unauthorized"}
	err := &login.GCOMStackError{Slug: "mystack", Status: 401, Cause: cause}

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Authentication failed", got.Summary)
	assert.Equal(t, `Grafana Cloud token rejected: GCOM returned 401 for stack "mystack"`, got.Details)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
}

func TestErrorToDetailedError_LoginGCOMStack404(t *testing.T) {
	cause := &cloud.GCOMHTTPError{Status: 404, Body: "not found"}
	err := &login.GCOMStackError{Slug: "mystack", Status: 404, Cause: cause}

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Resource not found", got.Summary)
	assert.Equal(t, `GCOM has no stack with slug "mystack"`, got.Details)
	require.NotEmpty(t, got.Suggestions)
	assert.Contains(t, strings.Join(got.Suggestions, "\n"), "mystack")
}

func TestErrorToDetailedError_StacksConflict409(t *testing.T) {
	tests := []struct {
		name            string
		wrap            string
		httpErr         *cloud.GCOMHTTPError
		wantSummary     string
		wantExitUsage   bool
		wantSuggestion  string
		notInSuggestion string
		wantDetail      string
	}{
		{
			name:            "create InvalidArgument with slug message",
			wrap:            "failed to create stack",
			httpErr:         &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"InvalidArgument","message":"Invalid slug my-gcx-eval specified"}`, Code: "InvalidArgument", Message: "Invalid slug my-gcx-eval specified"},
			wantSummary:     "Invalid stack request",
			wantExitUsage:   true,
			wantSuggestion:  "Choose a different slug",
			notInSuggestion: "lowercase letters",
		},
		{
			name:            "create InvalidArgument with non-slug message",
			wrap:            "failed to create stack",
			httpErr:         &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"InvalidArgument","message":"Invalid region narnia specified"}`, Code: "InvalidArgument", Message: "Invalid region narnia specified"},
			wantSummary:     "Invalid stack request",
			wantExitUsage:   true,
			wantSuggestion:  "--dry-run",
			notInSuggestion: "Choose a different slug",
		},
		{
			name:           "create Conflict code with duplicate-looking message",
			wrap:           "failed to create stack",
			httpErr:        &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"Conflict","message":"that url has already been taken"}`, Code: "Conflict", Message: "that url has already been taken"},
			wantSummary:    "Resource conflict",
			wantSuggestion: "Choose a different slug",
		},
		{
			name:           "create code-less duplicate-looking message",
			wrap:           "failed to create stack",
			httpErr:        &cloud.GCOMHTTPError{Status: 409, Body: `{"message":"slug already taken"}`, Message: "slug already taken"},
			wantSummary:    "Resource conflict",
			wantSuggestion: "Choose a different slug",
		},
		{
			name:           "create unknown nonempty code keeps slug remediation",
			wrap:           "failed to create stack",
			httpErr:        &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"SomethingNew","message":"conflicting state"}`, Code: "SomethingNew", Message: "conflicting state"},
			wantSummary:    "Resource conflict",
			wantSuggestion: "Choose a different slug",
		},
		{
			name:           "create non-JSON body keeps slug remediation",
			wrap:           "failed to create stack",
			httpErr:        &cloud.GCOMHTTPError{Status: 409, Body: `<html>bad gateway</html>`},
			wantSummary:    "Resource conflict",
			wantSuggestion: "Choose a different slug",
		},
		{
			name:            "update InvalidArgument is a usage error too",
			wrap:            "failed to update stack",
			httpErr:         &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"InvalidArgument","message":"Invalid name specified"}`, Code: "InvalidArgument", Message: "Invalid name specified"},
			wantSummary:     "Invalid stack request",
			wantExitUsage:   true,
			wantSuggestion:  "--dry-run",
			notInSuggestion: "Choose a different slug",
		},
		{
			name:            "update conflict has no slug remediation",
			wrap:            "failed to update stack",
			httpErr:         &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"Conflict","message":"conflict"}`, Code: "Conflict", Message: "conflict"},
			wantSummary:     "Resource conflict",
			wantSuggestion:  "List existing stacks",
			notInSuggestion: "--slug",
		},
		{
			name:            "list InvalidArgument stays a generic conflict",
			wrap:            "failed to list stacks",
			httpErr:         &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"InvalidArgument","message":"Invalid arguments"}`, Code: "InvalidArgument", Message: "Invalid arguments"},
			wantSummary:     "Resource conflict",
			notInSuggestion: "--slug",
		},
		{
			name:           "delete keeps delete-protection mapping",
			wrap:           "failed to delete stack",
			httpErr:        &cloud.GCOMHTTPError{Status: 409, Body: `{"code":"Conflict","message":"instance is protected"}`, Code: "Conflict", Message: "instance is protected"},
			wantSummary:    "Resource conflict",
			wantSuggestion: "--no-delete-protection",
			wantDetail:     "The stack has delete protection enabled.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("%s: %w", tt.wrap, tt.httpErr)

			got := toDetailedError(t, err)

			require.NotNil(t, got)
			assert.Equal(t, tt.wantSummary, got.Summary)
			if tt.wantExitUsage {
				require.NotNil(t, got.ExitCode)
				assert.Equal(t, gcxerrors.ExitUsageError, *got.ExitCode)
				assert.Equal(t, docs.CloudAPI, got.DocsLink)
			} else {
				assert.Nil(t, got.ExitCode, "non-usage 409s keep the default exit code")
			}
			if tt.httpErr.Message != "" {
				assert.True(t, strings.HasPrefix(got.Details, tt.httpErr.Message),
					"details must lead with GCOM's message, got %q", got.Details)
			}
			if tt.wantDetail != "" {
				assert.Contains(t, got.Details, tt.wantDetail)
			}
			joined := strings.Join(got.Suggestions, "\n")
			if tt.wantSuggestion != "" {
				assert.Contains(t, joined, tt.wantSuggestion)
			}
			if tt.notInSuggestion != "" {
				assert.NotContains(t, joined, tt.notInSuggestion)
			}
		})
	}
}

func TestErrorToDetailedError_StacksAuthErrors(t *testing.T) {
	for _, tt := range []struct {
		status      int
		wantSummary string
	}{
		{403, "Authorization failed"},
		{401, "Authentication failed"},
	} {
		t.Run(tt.wantSummary, func(t *testing.T) {
			err := fmt.Errorf("failed to create stack: %w",
				&cloud.GCOMHTTPError{Status: tt.status, Body: `{"message":"token lacks stacks scopes"}`, Message: "token lacks stacks scopes"})

			got := toDetailedError(t, err)

			require.NotNil(t, got)
			assert.Equal(t, tt.wantSummary, got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			assert.True(t, strings.HasPrefix(got.Details, "token lacks stacks scopes"),
				"auth details must lead with GCOM's message, got %q", got.Details)
		})
	}
}

func TestErrorToDetailedError_NonStacks409NotClaimed(t *testing.T) {
	err := fmt.Errorf("failed to frobnicate: %w",
		&cloud.GCOMHTTPError{Status: 409, Body: `{"code":"Conflict"}`, Code: "Conflict"})

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.NotEqual(t, "Resource conflict", got.Summary)
	assert.NotEqual(t, "Invalid stack request", got.Summary)
}

func TestErrorToDetailedError_LoginHealthCheckAuth(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprintf("status %d", status), func(t *testing.T) {
			err := &login.HealthCheckError{
				Server: "https://example.grafana.net",
				Status: status,
				Cause:  errors.New("unauthorized"),
			}

			got := toDetailedError(t, err)

			require.NotNil(t, got)
			if status == http.StatusForbidden {
				assert.Equal(t, "Authorization failed", got.Summary)
				assert.Equal(t, fmt.Sprintf("Grafana access denied: /api/health returned %d for https://example.grafana.net", status), got.Details)
				assert.NotContains(t, strings.Join(got.Suggestions, " "), "gcx login")
			} else {
				assert.Equal(t, "Authentication failed", got.Summary)
				assert.Equal(t, fmt.Sprintf("Grafana token rejected: /api/health returned %d for https://example.grafana.net", status), got.Details)
			}
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
		})
	}
}

func TestErrorToDetailedError_LoginHealthCheckUnreachable(t *testing.T) {
	err := &login.HealthCheckError{
		Server: "https://example.grafana.net",
		Status: 0,
		Cause:  errors.New("dial tcp: connection refused"),
	}

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Network error", got.Summary)
	assert.Equal(t, "Grafana server https://example.grafana.net unreachable: dial tcp: connection refused", got.Details)
	assert.Nil(t, got.ExitCode, "transport failures should not map to auth exit code")
}

func TestErrorToDetailedError_LoginK8sDiscovery(t *testing.T) {
	err := &login.K8sDiscoveryError{
		Server: "https://example.grafana.net",
		Cause:  errors.New("the server could not find the requested resource"),
	}

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, "Endpoint not available", got.Summary)
	assert.Equal(t, "Kubernetes-style API unavailable: the server could not find the requested resource", got.Details)
	require.NotEmpty(t, got.Suggestions)
}

func TestErrorToDetailedError_LoginVersionCheck(t *testing.T) {
	v, _ := semver.NewVersion("11.5.0")
	err := &login.VersionCheckError{Cause: &grafana.VersionIncompatibleError{Version: v}}

	got := toDetailedError(t, err)

	require.NotNil(t, got)
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitVersionIncompatible, *got.ExitCode)
}

func TestConvertFleetHTTPErrors(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantSummary  string
		wantAuthExit bool
	}{
		{
			name:         "401 from fleet management",
			err:          fmt.Errorf("clusters list: %w", &fleet.HTTPError{Status: 401, Path: "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation", Body: `{"message":"Plugin not found"}`}),
			wantSummary:  "Authentication failed",
			wantAuthExit: true,
		},
		{
			name:         "403 from fleet management",
			err:          fmt.Errorf("clusters list: %w", &fleet.HTTPError{Status: 403, Path: "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation", Body: `{"message":"Plugin is not enabled"}`}),
			wantSummary:  "Authorization failed",
			wantAuthExit: true,
		},
		{
			name: "404 for a missing resource is not handled by this converter",
			err:  &fleet.HTTPError{Status: 404, Path: "/foo", Body: `{"code":"not_found","message":"pipeline not found"}`},
		},
		{
			name:        "404 for a missing plugin route reports the plugin",
			err:         &fleet.HTTPError{Status: 404, Path: "/foo", Body: `{"message":"plugin route match not found"}`},
			wantSummary: "Endpoint not available",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			de := toDetailedError(t, tc.err)
			if tc.wantSummary == "" {
				return // just verify no panic
			}
			assert.Equal(t, tc.wantSummary, de.Summary)
			if tc.wantAuthExit {
				require.NotNil(t, de.ExitCode)
				// ExitAuthFailure should be non-zero
				assert.NotZero(t, *de.ExitCode)
			}
		})
	}
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

func TestErrorToDetailedError_WaitTimeoutEmittedSuppressesEnvelope(t *testing.T) {
	// ErrWaitTimeoutEmitted must be recognised FIRST in the converter
	// chain and suppress the secondary DetailedError JSON envelope.
	// ErrorToDetailedError must return nil so that main.go exits 1 without
	// writing a second JSON document to stdout.
	err := fmt.Errorf("clusters wait: %w", instrumentation.ErrWaitTimeoutEmitted)

	got := toDetailedError(t, err)

	// nil means "already handled; suppress secondary output" — matches
	// the convertLinterErrors(ErrTestsFailed) precedent.
	assert.Nil(t, got, "ErrWaitTimeoutEmitted must suppress the DetailedError envelope (return nil)")
}

func TestErrorToDetailedError_AlreadyReportedSuppressesEnvelope(t *testing.T) {
	err := fmt.Errorf("config check failed: %w", gcxerrors.ErrAlreadyReported)

	got := toDetailedError(t, err)

	assert.Nil(t, got, "an already-rendered diagnostic must not produce a second error envelope")
}

func TestErrorToDetailedError_WaitTimeoutEmittedBeforeOtherConverters(t *testing.T) {
	// Verify that the sentinel converter runs BEFORE other converters that might
	// also match. Wrap ErrWaitTimeoutEmitted alongside a usage error; the
	// sentinel must win and return nil, not the usage error's DetailedError.
	sentinelErr := fmt.Errorf("apps wait: %w", instrumentation.ErrWaitTimeoutEmitted)

	got := toDetailedError(t, sentinelErr)

	assert.Nil(t, got,
		"sentinel converter must fire before generic converters — expected nil, not %+v", got)
}

func TestErrorToDetailedError_MutuallyExclusiveFlagsSentinel(t *testing.T) {
	// Wrapping the typed sentinel must produce the "Invalid command usage"
	// envelope with the wrapped message as details. A bare error whose text
	// happens to contain "mutually exclusive" must NOT match — only the typed
	// sentinel triggers this converter.
	wrapped := fmt.Errorf("--costmetrics and --no-costmetrics: %w", instrumentation.ErrMutuallyExclusiveFlags)

	got := toDetailedError(t, wrapped)

	require.NotNil(t, got)
	assert.Equal(t, "Invalid command usage", got.Summary)
	assert.Contains(t, got.Details, "--costmetrics and --no-costmetrics")
	require.NotNil(t, got.ExitCode)
	assert.Equal(t, gcxerrors.ExitUsageError, *got.ExitCode)

	// Bare string must fall through to the generic fallback (no Suggestions,
	// no typed-error semantics).
	bare := errors.New("--foo and --bar are mutually exclusive")
	bareGot := toDetailedError(t, bare)
	require.NotNil(t, bareGot)
	assert.NotEqual(t, "Invalid command usage", bareGot.Summary,
		"converter must only match the typed sentinel, not arbitrary strings")
}

// TestErrorToDetailedError_UnknownFieldSelectionError verifies that
// UnknownFieldSelectionError is converted to a DetailedError with:
//   - Summary: "Invalid command usage"
//   - ExitCode: 2 (ExitUsageError)
//   - Details containing the offending field names
//   - A suggestion to run --json list
func TestErrorToDetailedError_UnknownFieldSelectionError(t *testing.T) {
	tests := []struct {
		name           string
		fields         []string
		wantInDetails  string
		wantExitCode   int
		wantSuggestion string
	}{
		{
			name:           "single unknown field",
			fields:         []string{"bogus"},
			wantInDetails:  "bogus",
			wantExitCode:   gcxerrors.ExitUsageError,
			wantSuggestion: "--json list",
		},
		{
			name:          "multiple unknown fields",
			fields:        []string{"foo", "bar"},
			wantInDetails: "foo",
			wantExitCode:  gcxerrors.ExitUsageError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := cmdoutput.UnknownFieldSelectionError{Fields: tc.fields}

			got := toDetailedError(t, err)

			require.NotNil(t, got)
			assert.Equal(t, "Invalid command usage", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, tc.wantExitCode, *got.ExitCode)
			assert.Contains(t, got.Details, tc.wantInDetails)
			if tc.wantSuggestion != "" {
				found := false
				for _, s := range got.Suggestions {
					if strings.Contains(s, tc.wantSuggestion) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected suggestion containing %q in %v", tc.wantSuggestion, got.Suggestions)
			}
		})
	}
}

// TestErrorToDetailedError_JQRuntimeError verifies that JQRuntimeError (a --jq
// expression failed against the actual output) is converted to a DetailedError
// with:
//   - Summary: "Invalid command usage"
//   - ExitCode: 2 (ExitUsageError)
//   - Details containing the gojq message and the output shape summary
//   - Suggestions for array iteration (arrays only) and --json list discovery
func TestErrorToDetailedError_JQRuntimeError(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		wantInDetails   []string
		wantSuggestions []string
		notSuggestions  []string
	}{
		{
			name: "array input includes element fields and iteration hint",
			err: cmdoutput.JQRuntimeError{
				Err:        errors.New(`cannot index array with "string"`),
				Shape:      "an array of 25 objects",
				Fields:     []string{"name", "ns"},
				MoreFields: 3,
				ArrayInput: true,
			},
			wantInDetails:   []string{`cannot index array with "string"`, "an array of 25 objects", "Element fields: name, ns (+3 more)"},
			wantSuggestions: []string{".[]", "--json list"},
		},
		{
			name: "object input has plain fields label and no iteration hint",
			err: cmdoutput.JQRuntimeError{
				Err:    errors.New("cannot index number with \"bar\""),
				Shape:  "an object",
				Fields: []string{"foo"},
			},
			wantInDetails:   []string{"an object", "Fields: foo"},
			wantSuggestions: []string{"--json list"},
			notSuggestions:  []string{".[]"},
		},
		{
			name: "scalar input omits field list",
			err: cmdoutput.JQRuntimeError{
				Err:   errors.New("cannot index number with \"foo\""),
				Shape: "a number",
			},
			wantInDetails:   []string{"a number"},
			wantSuggestions: []string{"--json list"},
		},
		{
			name: "wrapped error still converts",
			err: fmt.Errorf("encode: %w", cmdoutput.JQRuntimeError{
				Err:        errors.New("cannot iterate over null"),
				Shape:      "an array of 2 objects",
				ArrayInput: true,
			}),
			wantInDetails:   []string{"cannot iterate over null", "an array of 2 objects"},
			wantSuggestions: []string{".[]"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)

			require.NotNil(t, got)
			assert.Equal(t, "Invalid command usage", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitUsageError, *got.ExitCode)
			for _, want := range tc.wantInDetails {
				assert.Contains(t, got.Details, want)
			}
			joined := strings.Join(got.Suggestions, "\n")
			for _, want := range tc.wantSuggestions {
				assert.Contains(t, joined, want)
			}
			for _, notWant := range tc.notSuggestions {
				assert.NotContains(t, joined, notWant)
			}
		})
	}
}

func TestErrorToDetailedError_UsageErrorExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "UsageError returns ExitUsageError",
			err:  fail.NewCommandUsageError(nil, "bad input", nil),
		},
		{
			name: "unknown command returns ExitUsageError",
			err:  errors.New(`unknown command "foo" for "gcx"`),
		},
		{
			name: "required flags returns ExitUsageError",
			err:  errors.New(`required flag(s) "datasource" not set`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)
			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode, "ExitCode should be set for usage errors")
			assert.Equal(t, gcxerrors.ExitUsageError, *got.ExitCode)
		})
	}
}

func TestErrorToDetailedError_PartialFailureExitCode(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantDetails string
	}{
		{
			name:        "push partial failure",
			err:         gcxerrors.NewPartialFailureError("push", 100, 10),
			wantDetails: "10 of 100 resource(s) failed to push",
		},
		{
			name:        "pull partial failure",
			err:         gcxerrors.NewPartialFailureError("pull", 50, 3),
			wantDetails: "3 of 50 resource(s) failed to pull",
		},
		{
			name:        "delete partial failure",
			err:         gcxerrors.NewPartialFailureError("delete", 20, 5),
			wantDetails: "5 of 20 resource(s) failed to delete",
		},
		{
			name:        "validate partial failure",
			err:         gcxerrors.NewPartialFailureError("validate", 30, 7),
			wantDetails: "7 of 30 resource(s) failed to validate",
		},
		{
			name:        "wrapped partial failure keeps the caller context after the counts",
			err:         fmt.Errorf("sync dashboards: %w", gcxerrors.NewPartialFailureError("push", 4, 1)),
			wantDetails: "1 of 4 resource(s) failed to push\n\nsync dashboards",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)
			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode, "ExitCode should be set for partial failures")
			assert.Equal(t, gcxerrors.ExitPartialFailure, *got.ExitCode)
			assert.Equal(t, "Partial failure", got.Summary)
			assert.Equal(t, tc.wantDetails, got.Details)
		})
	}
}

func TestPartialFailureError_Message(t *testing.T) {
	err := gcxerrors.NewPartialFailureError("push", 100, 10)
	assert.Equal(t, "10 resource(s) failed to push", err.Error())
}

func TestErrorToDetailedError_ValueTypedPreservesExitCode(t *testing.T) {
	two := 2
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "bare value-typed DetailedError preserves ExitCode",
			err:  gcxerrors.DetailedError{ExitCode: &two, Summary: "test"},
		},
		{
			name: "bare pointer-typed DetailedError preserves ExitCode",
			err:  &gcxerrors.DetailedError{ExitCode: &two, Summary: "test"},
		},
		{
			name: "value-typed DetailedError wrapped via fmt.Errorf preserves ExitCode",
			err:  fmt.Errorf("context: %w", gcxerrors.DetailedError{ExitCode: &two, Summary: "test"}),
		},
		{
			name: "pointer-typed DetailedError wrapped via fmt.Errorf preserves ExitCode",
			err:  fmt.Errorf("context: %w", &gcxerrors.DetailedError{ExitCode: &two, Summary: "test"}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toDetailedError(t, tc.err)

			require.NotNil(t, got)
			require.NotNil(t, got.ExitCode, "ExitCode must not be nil — value-typed DetailedError must propagate ExitCode")
			assert.Equal(t, 2, *got.ExitCode, "ExitCode must equal the original value, not nil or 1")
		})
	}
}

func TestErrorToDetailedError_EmittedErrorSuppressesEnvelope(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "bare EmittedError",
			err:  gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New("2 failed")),
		},
		{
			name: "wrapped EmittedError",
			err:  fmt.Errorf("push: %w", gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, nil)),
		},
		{
			name: "chain carrying both a DetailedError and an EmittedError",
			err: &gcxerrors.DetailedError{
				Summary: "outer",
				Parent:  gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New("inner")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Nil(t, toDetailedError(t, tt.err),
				"an EmittedError anywhere in the chain must suppress the secondary envelope")
		})
	}
}

// TestErrorToDetailedError_KeychainLocked asserts that a locked OS keychain
// produces an actionable envelope, and that the other credentials sentinels do
// not claim it. The locked error stays fatal, because gcx must not write the
// secret in plaintext when a real keychain exists.
func TestErrorToDetailedError_KeychainLocked(t *testing.T) {
	lockedErr := fmt.Errorf("%w: %s", credentials.ErrLocked,
		"failed to unlock correct collection '/org/freedesktop/secrets/collection/login'")

	tests := []struct {
		name        string
		err         error
		wantLocked  bool
		wantSummary string
	}{
		{
			name:       "bare ErrLocked",
			err:        credentials.ErrLocked,
			wantLocked: true,
		},
		{
			name: "deeply wrapped ErrLocked",
			err: fmt.Errorf("writing config: %w",
				fmt.Errorf("inspect keychain entry for %q field %q: %w",
					"stack:opstest", "oauth-token", lockedErr)),
			wantLocked: true,
		},
		{
			name:        "ErrUnavailable is an actionable unavailable keychain",
			err:         fmt.Errorf("writing config: %w", credentials.ErrUnavailable),
			wantSummary: "Keychain unavailable",
		},
		{
			// ErrDisabled wraps ErrUnavailable, so it must be checked ahead of
			// ErrUnavailable or it silently gets the "Keychain unavailable"
			// envelope that convert.go deliberately refuses it. A deliberate
			// GCX_KEYCHAIN=off opt-out still falls back to plaintext, so it
			// must fall through to the generic error envelope instead.
			name:        "ErrDisabled must not shadow into the unavailable-keychain envelope",
			err:         fmt.Errorf("writing config: %w", credentials.ErrDisabled),
			wantSummary: "Unexpected error",
		},
		{
			name:       "ErrNotFound is not a locked keychain",
			err:        fmt.Errorf("writing config: %w", credentials.ErrNotFound),
			wantLocked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDetailedError(t, tt.err)
			require.NotNil(t, got)

			// ErrDisabled must be tested for explicitly, and ahead of
			// ErrUnavailable: ErrDisabled wraps ErrUnavailable, so a check
			// that only tests errors.Is(err, ErrUnavailable) would also match
			// ErrDisabled and assert the wrong envelope.
			if errors.Is(tt.err, credentials.ErrDisabled) {
				require.NotEmpty(t, tt.wantSummary, "test row must pin an exact summary")
				assert.Equal(t, tt.wantSummary, got.Summary)
				assert.NotEqual(t, "Keychain unavailable", got.Summary,
					"a deliberate GCX_KEYCHAIN=off opt-out must get the generic error envelope, not the unavailable-keychain one")
				assert.Equal(t, tt.err.Error(), got.Details)
				return
			}

			if errors.Is(tt.err, credentials.ErrUnavailable) {
				require.NotEmpty(t, tt.wantSummary, "test row must pin an exact summary")
				assert.Equal(t, tt.wantSummary, got.Summary)
				assert.Equal(t,
					"The OS keychain is unavailable. gcx did not fall back to plaintext credential storage.",
					got.Details)
				require.ErrorIs(t, got.Parent, credentials.ErrUnavailable)
				assert.NotErrorIs(t, got.Parent, credentials.ErrLocked)
				assert.Contains(t, strings.Join(got.Suggestions, "\n"), "GCX_KEYCHAIN=off")
				assert.Contains(t, strings.Join(got.Suggestions, "\n"), "Plaintext credentials are stored on disk")
				return
			}

			if !tt.wantLocked {
				assert.NotEqual(t, "Keychain locked", got.Summary)
				return
			}

			assert.Equal(t, "Keychain locked", got.Summary)
			assert.Equal(t,
				"The OS keychain is reachable, but it is locked or cannot be unlocked in this session. gcx does not fall back to a plaintext credential.",
				got.Details)
			require.Error(t, got.Parent)
			require.ErrorIs(t, got.Parent, credentials.ErrLocked)
			assert.Equal(t, docs.Keychain, got.DocsLink)
			// convert_internal_test.go pins the per-platform suggestions.
			assert.NotEmpty(t, got.Suggestions)
			assert.NotContains(t, strings.Join(got.Suggestions, "\n"), "GCX_KEYCHAIN=off")
		})
	}
}

func TestErrorToDetailedError_RestrictedCredentialSession(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "direct credential-store failure",
			err:  fmt.Errorf("write config: %w", credentials.ErrRestrictedSession),
		},
		{
			name: "OAuth refresh preflight failure",
			err:  fmt.Errorf("request failed: %w: %w", auth.ErrCredentialPersistencePreflight, credentials.ErrRestrictedSession),
		},
		{
			name: "OAuth login preflight failure",
			err:  fmt.Errorf("login failed: %w: %w", login.ErrCredentialPersistencePreflight, credentials.ErrRestrictedSession),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDetailedError(t, tt.err)
			require.NotNil(t, got)
			assert.Equal(t, "OS credential store access is restricted", got.Summary)
			assert.NotEqual(t, "Keychain locked", got.Summary)
			assert.Equal(t, docs.Keychain, got.DocsLink)
		})
	}
}

func TestBasicAuthCheckError(t *testing.T) {
	t.Run("empty identity", func(t *testing.T) {
		result := toDetailedError(t, &login.BasicAuthCheckError{})
		assert.Equal(t, "Authentication failed", result.Summary)
		require.NotNil(t, result.ExitCode)
		assert.Equal(t, gcxerrors.ExitAuthFailure, *result.ExitCode)
		assert.Contains(t, strings.Join(result.Suggestions, " "), "anonymous access")
	})
	for _, tt := range []struct {
		status  int
		summary string
	}{
		{401, "Authentication failed"},
		{403, "Authorization failed"},
		{404, "API error"},
		{500, "API error"},
		{0, "Network error"},
	} {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			err := &login.BasicAuthCheckError{Status: tt.status}
			if tt.status == 0 {
				err.Cause = errors.New("TLS handshake failed")
			}
			result := toDetailedError(t, err)
			assert.Equal(t, tt.summary, result.Summary)
			if tt.status == 401 || tt.status == 403 {
				require.NotNil(t, result.ExitCode)
				assert.Equal(t, gcxerrors.ExitAuthFailure, *result.ExitCode)
			} else {
				assert.Nil(t, result.ExitCode)
			}
			if tt.status != 401 {
				assert.NotContains(t, strings.Join(result.Suggestions, " "), "password")
			}
			if tt.status == 0 {
				assert.Contains(t, result.Details, "TLS handshake failed")
				assert.ErrorIs(t, err, err.Cause)
			}
		})
	}
}

// Exercise the same normalization and wrapping that dynamic-client callers use.
func TestErrorToDetailedError_DynamicClient(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantSummary string
		wantExit    int
	}{
		{"refused", &url.Error{Op: "Get", URL: "http://127.0.0.1:1", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}, "Network error", gcxerrors.ExitGeneralError},
		{"cancelled", &url.Error{Op: "Get", URL: "http://example.invalid", Err: context.Canceled}, "Operation cancelled", gcxerrors.ExitCancelled},
		{"deadline exceeded", &url.Error{Op: "Get", URL: "http://example.invalid", Err: context.DeadlineExceeded}, "Network error", gcxerrors.ExitGeneralError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("get dashboard: %w", dynamic.ParseStatusError(tc.err))
			got := toDetailedError(t, err)
			assert.Equal(t, tc.wantSummary, got.Summary)
			exit := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				exit = *got.ExitCode
			}
			assert.Equal(t, tc.wantExit, exit)
			assert.ErrorIs(t, got, tc.err)
		})
	}
}

func TestErrorToDetailedError_APIStatusVocabulary(t *testing.T) {
	tests := []struct {
		code        int32
		reason      metav1.StatusReason
		wantSummary string
		wantExit    int
	}{
		{401, metav1.StatusReasonUnauthorized, "Authentication failed", gcxerrors.ExitAuthFailure},
		{403, metav1.StatusReasonForbidden, "Authorization failed", gcxerrors.ExitAuthFailure},
		{404, metav1.StatusReasonNotFound, "Resource not found", gcxerrors.ExitGeneralError},
		{409, metav1.StatusReasonConflict, "Resource conflict", gcxerrors.ExitGeneralError},
		{502, metav1.StatusReasonInternalError, "API error", gcxerrors.ExitGeneralError},
		{500, "", "API error", gcxerrors.ExitGeneralError},
		{401, "", "Authentication failed", gcxerrors.ExitAuthFailure},
		{403, "", "Authorization failed", gcxerrors.ExitAuthFailure},
		{404, "", "Resource not found", gcxerrors.ExitGeneralError},
		{409, "", "Resource conflict", gcxerrors.ExitGeneralError},
	}
	for _, tc := range tests {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%s/dynamic=%t", tc.code, tc.reason, wrapped), func(t *testing.T) {
				var err error = &k8sapi.StatusError{ErrStatus: metav1.Status{Status: metav1.StatusFailure, Code: tc.code, Reason: tc.reason, Message: "server message"}}
				if wrapped {
					err = dynamic.ParseStatusError(err)
				}
				err = fmt.Errorf("get dashboard: %w", err)
				got := toDetailedError(t, err)
				assert.Equal(t, tc.wantSummary, got.Summary)
				exit := gcxerrors.ExitGeneralError
				if got.ExitCode != nil {
					exit = *got.ExitCode
				}
				assert.Equal(t, tc.wantExit, exit)
				reason := string(tc.reason)
				if reason == "" {
					reason = http.StatusText(int(tc.code))
				}
				assert.Equal(t, fmt.Sprintf("get dashboard: %d %s: server message", tc.code, reason), got.Parent.Error())
				require.ErrorIs(t, got, err)
				assert.Contains(t, got.Parent.Error(), "server message")
				var rendered bytes.Buffer
				require.NoError(t, got.WriteJSON(&rendered, exit))
				assert.Contains(t, rendered.String(), "server message")
				assert.Contains(t, rendered.String(), "get dashboard")
			})
		}
	}
}

func TestErrorToDetailedError_APIStatusMessage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		want    string
	}{
		{"server explanation", "the server has asked for the client to provide credentials", "401 Unauthorized: the server has asked for the client to provide credentials"},
		{"reason only", "Unauthorized", "401 Unauthorized"},
		{"empty message", "", "401 Unauthorized"},
	} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dynamic=%t", tc.name, wrapped), func(t *testing.T) {
				var err error = &k8sapi.StatusError{ErrStatus: metav1.Status{Code: 401, Reason: metav1.StatusReasonUnauthorized, Message: tc.message}}
				if wrapped {
					err = dynamic.ParseStatusError(err)
				}
				got := toDetailedError(t, err)
				assert.Equal(t, tc.want, got.Parent.Error())
				require.ErrorIs(t, got, err)
				var status k8sapi.APIStatus
				require.ErrorAs(t, got, &status)
				assert.Equal(t, int32(401), status.Status().Code)
				assert.Equal(t, 1, strings.Count(got.Error(), "401 Unauthorized"))
				assert.NotContains(t, got.Error(), "code 401")
				var rendered bytes.Buffer
				require.NoError(t, got.WriteJSON(&rendered, gcxerrors.ExitAuthFailure))
				assert.Contains(t, rendered.String(), tc.want)
				assert.Equal(t, 1, strings.Count(rendered.String(), "401 Unauthorized"))
			})
		}
	}
}

func TestErrorToDetailedError_APIStatusCallerContext(t *testing.T) {
	for _, tc := range []struct {
		message string
		prefix  string
		suffix  string
		want    string
	}{
		{"Unauthorized", "get dashboard Unauthorized: ", "", "get dashboard Unauthorized: 401 Unauthorized"},
		{"", "get dashboard: ", "", "get dashboard: 401 Unauthorized"},
		{"server message", "get dashboard: ", " (retry failed)", "get dashboard: 401 Unauthorized: server message (retry failed)"},
	} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dynamic=%t", tc.prefix+tc.message, wrapped), func(t *testing.T) {
				var err error = &k8sapi.StatusError{ErrStatus: metav1.Status{Code: 401, Reason: metav1.StatusReasonUnauthorized, Message: tc.message}}
				if wrapped {
					err = dynamic.ParseStatusError(err)
				}
				err = fmt.Errorf("%s%w%s", tc.prefix, err, tc.suffix)
				got := toDetailedError(t, err)
				assert.Equal(t, tc.want, got.Parent.Error())
				require.ErrorIs(t, got, err)
			})
		}
	}
}

func TestErrorToDetailedError_DynamicAuthenticationRenewal(t *testing.T) {
	for _, cause := range []error{auth.ErrRefreshTokenExpired, auth.ErrRefreshTokenMissing} {
		t.Run(cause.Error(), func(t *testing.T) {
			err := &url.Error{Op: "Get", URL: "https://example.invalid/apis", Err: fmt.Errorf("token refresh failed: %w", cause)}
			got := toDetailedError(t, dynamic.ParseStatusError(err))
			assert.Equal(t, "Authentication failed", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			require.ErrorIs(t, got, cause)
			assert.Contains(t, got.Error(), cause.Error())
			assert.Equal(t, []string{"Run `gcx login` to re-authenticate"}, got.Suggestions)
		})
	}
}

func TestErrorToDetailedError_InvalidResourceSelector(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped=%t", wrapped), func(t *testing.T) {
			var err error = resources.InvalidSelectorError{Command: "dashboards////", Err: "too many selector segments"}
			if wrapped {
				err = fmt.Errorf("read resources: %w", err)
			}
			got := toDetailedError(t, err)
			assert.Equal(t, "Invalid command usage", got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitUsageError, *got.ExitCode)
			assert.Equal(t, 1, strings.Count(got.Error(), "dashboards////"))
			assert.NotContains(t, got.Error(), "gcx resources get")
			var rendered bytes.Buffer
			require.NoError(t, got.WriteJSON(&rendered, gcxerrors.ExitUsageError))
			assert.Contains(t, rendered.String(), "too many selector segments")
		})
	}
}

func TestErrorToDetailedError_UnsupportedResource(t *testing.T) {
	for _, prefix := range []string{"", "server does not support dashboards resource (api-version: v99)", "no api-version specified, server does not expose the dashboards resource", "resources get", "resources delete", "resources push", "resources validate", "resources schemas", "resources examples"} {
		t.Run(prefix, func(t *testing.T) {
			cause := &resources.UnsupportedResourceError{Selector: "dashboards", Reason: "the server does not support this resource"}
			var err error = cause
			if prefix != "" {
				err = fmt.Errorf("%s: %w", prefix, err)
			}
			got := toDetailedError(t, err)
			require.Equal(t, "Endpoint not available", got.Summary)
			exit := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				exit = *got.ExitCode
			}
			require.Equal(t, gcxerrors.ExitGeneralError, exit)
			require.ErrorIs(t, got, cause)
			require.Empty(t, got.Suggestions)
			var rendered bytes.Buffer
			require.NoError(t, got.WriteJSON(&rendered, exit))
			require.Contains(t, rendered.String(), "the server does not support this resource")
			require.NotContains(t, rendered.String(), "Invalid command usage")
		})
	}
}

func TestErrorToDetailedError_SharedCommandUsage(t *testing.T) {
	cmd := &cobra.Command{Use: "list [flags]"}
	parent := &cobra.Command{Use: "dashboards"}
	root := &cobra.Command{Use: "gcx"}
	root.AddCommand(parent)
	parent.AddCommand(cmd)
	cause := errors.New("--limit must be >= 0")
	got := toDetailedError(t, gcxerrors.NewCommandUsageError(cmd, "", cause))
	require.Equal(t, "Invalid command usage", got.Summary)
	require.NotNil(t, got.ExitCode)
	require.Equal(t, gcxerrors.ExitUsageError, *got.ExitCode)
	require.Contains(t, got.Details, "--limit must be >= 0")
	require.Contains(t, got.Details, "Expected:\n  gcx dashboards list [flags]")
	require.Equal(t, []string{"Run 'gcx dashboards list --help' for full usage and examples"}, got.Suggestions)
}

// TestConvertBrowserCancelled keeps a consent-page Cancel visible: exit code 5
// with a message, even though the root command exits silently for a plain
// context cancellation.
func TestConvertBrowserCancelled(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("OAuth flow failed: %w", auth.ErrBrowserCancelled)
	require.NotErrorIs(t, err, context.Canceled)

	det := toDetailedError(t, err)
	require.NotNil(t, det)
	assert.Equal(t, "Operation cancelled", det.Summary)
	assert.Contains(t, det.Details, "cancelled in the browser")
	require.NotNil(t, det.ExitCode)
	assert.Equal(t, gcxerrors.ExitCancelled, *det.ExitCode)
}

// TestConvertOAuthExchangeErrors gives a first-time user a next step when the
// token exchange hits a rate limit or a service error. Other statuses keep the
// generic rendering.
func TestConvertOAuthExchangeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status        int
		wantDetail    string
		wantSuggested string
	}{
		{status: 429, wantDetail: "Grafana Cloud is rate limiting logins", wantSuggested: "Wait a minute, then run gcx login again"},
		{status: 503, wantDetail: "Grafana Cloud could not finish the login", wantSuggested: "Run gcx login again in a few minutes"},
		{status: 401},
	}
	for _, tc := range tests {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			t.Parallel()

			exchangeErr := &auth.ExchangeStatusError{StatusCode: tc.status, Path: "/api/cli/v1/auth/exchange"}
			err := fmt.Errorf("OAuth flow failed: token exchange failed: %w", exchangeErr)
			det := toDetailedError(t, err)
			require.NotNil(t, det)
			if tc.wantDetail == "" {
				assert.NotContains(t, det.Details, "cannot be reused")
				assert.Empty(t, det.Suggestions)
				return
			}
			// The summary stays in the approved vocabulary (docs/design/errors.md);
			// the specific cause goes in the details.
			assert.Equal(t, "API error", det.Summary)
			assert.Contains(t, det.Details, tc.wantDetail)
			assert.Contains(t, det.Details, "cannot be reused")
			assert.Equal(t, []string{tc.wantSuggested}, det.Suggestions)
		})
	}
}

// TestSignupIncompleteErrorKeepsTheFailure pins the rendering of a signup that
// failed once its browser step had started: the failure keeps its own summary,
// details, suggestions and exit code, and the recovery comes first without
// suggesting signup again.
func TestSignupIncompleteErrorKeepsTheFailure(t *testing.T) {
	t.Parallel()

	keychain := gcxerrors.DetailedError{
		Summary:     "Keychain locked",
		Details:     "the login keychain is locked",
		Suggestions: []string{"Unlock the keychain"},
		ExitCode:    new(gcxerrors.ExitAuthFailure),
	}
	const connect = "gcx login default --server https://mystack.grafana.net --oauth"
	const signIn = "gcx login default --cloud --oauth"
	tests := []struct {
		name         string
		err          *login.SignupIncompleteError
		wantSummary  string
		wantExitCode *int
		wantNote     string
		wantFirst    string
		wantKept     string
	}{
		{
			name:        "a save failure",
			err:         &login.SignupIncompleteError{Err: fmt.Errorf("saving: %w", keychain), Server: "https://mystack.grafana.net", Recovery: connect},
			wantSummary: "Keychain locked", wantExitCode: keychain.ExitCode,
			wantNote:  "Grafana Cloud account and the stack https://mystack.grafana.net exist",
			wantFirst: "Once the cause above is fixed, connect gcx to the new stack: " + connect,
			wantKept:  "Unlock the keychain",
		},
		{
			name:        "a stack that is still starting",
			err:         &login.SignupIncompleteError{Err: &login.HealthCheckError{Server: "https://mystack.grafana.net", Status: 503, Cause: errors.New("unavailable")}, Server: "https://mystack.grafana.net", Recovery: connect},
			wantSummary: "Network error",
			wantNote:    "A new stack can take a few minutes to finish starting",
			wantFirst:   "Wait a few minutes, then connect gcx to the new stack: " + connect,
		},
		{
			name:        "a cancel on the Connect gcx page",
			err:         &login.SignupIncompleteError{Err: fmt.Errorf("OAuth flow failed: %w", auth.ErrBrowserCancelled), Recovery: signIn},
			wantSummary: "Operation cancelled", wantExitCode: new(gcxerrors.ExitCancelled),
			wantNote:  "If you already created your Grafana Cloud account in the browser, it exists",
			wantFirst: "Sign in instead of signing up again, and choose the new stack: " + signIn,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			det := toDetailedError(t, tc.err)
			require.NotNil(t, det)
			assert.Equal(t, tc.wantSummary, det.Summary)
			assert.Equal(t, tc.wantExitCode, det.ExitCode)
			assert.Contains(t, det.Details, tc.wantNote)
			require.NotEmpty(t, det.Suggestions)
			assert.Equal(t, tc.wantFirst, det.Suggestions[0])
			if tc.wantKept != "" {
				assert.Contains(t, det.Suggestions, tc.wantKept)
				assert.Contains(t, det.Details, keychain.Details)
			}
			assert.NotContains(t, strings.Join(det.Suggestions, "\n"), "gcx signup")
		})
	}
	// The inner error's own suggestions are not changed in place.
	assert.Equal(t, []string{"Unlock the keychain"}, keychain.Suggestions)
}

// TestSignupIncompleteErrorKeepsAWrappedCause pins that signup's note does not
// hide a cause that the fallback converter keeps only as Parent, such as a
// busy callback port. JSON output reads Details and falls back to Parent only
// when Details is empty, so an agent would otherwise get the note and the
// recovery but not why the signup stopped. Text shows the cause once.
func TestSignupIncompleteErrorKeepsAWrappedCause(t *testing.T) {
	const cause = "callback port 54322 unavailable: listen tcp 127.0.0.1:54322: bind: address already in use"
	const signIn = "gcx login default --cloud --oauth --oauth-callback-port 54322"
	err := &login.SignupIncompleteError{
		Err:      fmt.Errorf("OAuth flow failed: %w", errors.New(cause)),
		Recovery: signIn,
	}

	det := toDetailedError(t, err)
	require.NotNil(t, det)
	assert.Equal(t, "Unexpected error", det.Summary)

	var buf bytes.Buffer
	require.NoError(t, det.WriteJSON(&buf, 1))
	var envelope struct {
		Error struct {
			Details     string   `json:"details"`
			Suggestions []string `json:"suggestions"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope), buf.String())
	assert.Contains(t, envelope.Error.Details, "If you already created your Grafana Cloud account in the browser")
	assert.Contains(t, envelope.Error.Details, cause)
	require.NotEmpty(t, envelope.Error.Suggestions)
	assert.Equal(t, "Sign in instead of signing up again, and choose the new stack: "+signIn, envelope.Error.Suggestions[0])

	assert.Equal(t, 1, strings.Count(det.Error(), "address already in use"), det.Error())
}
