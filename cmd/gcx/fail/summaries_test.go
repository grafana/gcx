package fail_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/login"
	cmdoutput "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func k8sStatus(code int32, reason metav1.StatusReason) error {
	return &k8sapi.StatusError{ErrStatus: metav1.Status{Status: metav1.StatusFailure, Code: code, Reason: reason, Message: "server message"}}
}

// TestConverterBranches pins one error per converter branch in cmd/gcx/fail:
// the summary each branch produces, which must be in the vocabulary, and its
// exit code.
func TestConverterBranches(t *testing.T) {
	grafana11 := semver.MustParse("11.2.0")
	tests := []struct {
		name        string
		err         error
		wantSummary string
		wantExit    int
	}{
		{"usage error", fail.NewCommandUsageError(nil, "bad input", nil), gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"cobra unknown command", errors.New(`unknown command "foo" for "gcx"`), gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"config validation", config.ValidationError{File: "config.yaml", Message: "bad value"}, gcxerrors.SummaryInvalidConfiguration, gcxerrors.ExitGeneralError},
		{"config unmarshal", config.UnmarshalError{File: "config.yaml", Err: errors.New("bad yaml")}, gcxerrors.SummaryInvalidConfiguration, gcxerrors.ExitGeneralError},
		{"context not found", config.ContextNotFound("ops", nil), gcxerrors.SummaryInvalidConfiguration, gcxerrors.ExitGeneralError},
		{"refresh token expired", fmt.Errorf("refresh: %w", auth.ErrRefreshTokenExpired), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"credential store restricted", fmt.Errorf("write: %w", credentials.ErrRestrictedSession), gcxerrors.SummaryCredentialStoreRestricted, gcxerrors.ExitGeneralError},
		{"keychain unavailable", fmt.Errorf("write: %w", credentials.ErrUnavailable), gcxerrors.SummaryKeychainUnavailable, gcxerrors.ExitGeneralError},
		{"keychain locked", fmt.Errorf("write: %w", credentials.ErrLocked), gcxerrors.SummaryKeychainLocked, gcxerrors.ExitGeneralError},
		{"network", &url.Error{Op: "Get", URL: "http://127.0.0.1:1", Err: errors.New("connection refused")}, gcxerrors.SummaryNetworkError, gcxerrors.ExitGeneralError},
		{"k8s 401", k8sStatus(401, metav1.StatusReasonUnauthorized), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"k8s 403", k8sStatus(403, metav1.StatusReasonForbidden), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"k8s 404", k8sStatus(404, metav1.StatusReasonNotFound), gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"k8s 409", k8sStatus(409, metav1.StatusReasonConflict), gcxerrors.SummaryResourceConflict, gcxerrors.ExitGeneralError},
		{"k8s 500", k8sStatus(500, metav1.StatusReasonInternalError), gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"unavailable endpoint", queryerror.New("tempo", "trace diff", 404, "404 page not found", "").WithAvailability(true, true), gcxerrors.SummaryEndpointNotAvailable, gcxerrors.ExitGeneralError},
		{"query 401", queryerror.New("prometheus", "query", 401, "unauthorized", ""), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"query 403", queryerror.New("prometheus", "query", 403, "forbidden", ""), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"query parse error", queryerror.New("prometheus", "query", 400, "parse error: unexpected", ""), gcxerrors.SummaryInvalidQuery, gcxerrors.ExitGeneralError},
		{"query 400 with operation", queryerror.New("loki", "labels query", 400, "bad request", ""), gcxerrors.SummaryInvalidQuery, gcxerrors.ExitGeneralError},
		{"query 400 without operation", queryerror.New("loki", "", 400, "bad request", ""), gcxerrors.SummaryInvalidQuery, gcxerrors.ExitGeneralError},
		{"query trace 404", queryerror.New("tempo", "get trace", 404, "trace not found", ""), gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"query 404", queryerror.New("loki", "query", 404, "not found", ""), gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"query 500 with operation", queryerror.New("loki", "query", 500, "boom", ""), gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"query 500 without operation", queryerror.New("loki", "", 500, "", ""), gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"datasource 401", &datasources.APIError{Operation: "list datasources", StatusCode: 401}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"datasource 403", &datasources.APIError{Operation: "list datasources", StatusCode: 403}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"datasource 404 with identifier", &datasources.APIError{Operation: "get datasource", Identifier: "uid1", StatusCode: 404}, gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"datasource 404", &datasources.APIError{StatusCode: 404}, gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"datasource 500 with operation", &datasources.APIError{Operation: "list datasources", StatusCode: 500}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"datasource 500", &datasources.APIError{StatusCode: 500}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"adaptive logs scope 401", fakeServiceAPIError{statusCode: 401, service: "Adaptive Logs", message: "invalid scope requested"}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"adaptive logs scope 403", fakeServiceAPIError{statusCode: 403, service: "Adaptive Logs", message: "invalid scope requested"}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"service 401", fakeServiceAPIError{statusCode: 401, service: "Knowledge Graph", message: "bad token"}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"service 403", fakeServiceAPIError{statusCode: 403, service: "Knowledge Graph", message: "denied"}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"service 404", fakeServiceAPIError{statusCode: 404, service: "Knowledge Graph", message: "rule not found"}, gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"service 500", fakeServiceAPIError{statusCode: 500, service: "Knowledge Graph", message: "boom"}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"service 500 without name", fakeServiceAPIError{statusCode: 500, message: "boom"}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"file not found", &fs.PathError{Op: "open", Path: "a.yaml", Err: os.ErrNotExist}, gcxerrors.SummaryFileNotFound, gcxerrors.ExitGeneralError},
		{"invalid path", &fs.PathError{Op: "open", Path: "a.yaml", Err: os.ErrInvalid}, gcxerrors.SummaryInvalidPath, gcxerrors.ExitGeneralError},
		{"file permission denied", &fs.PathError{Op: "open", Path: "a.yaml", Err: os.ErrPermission}, gcxerrors.SummaryFileAccessDenied, gcxerrors.ExitGeneralError},
		{"unsupported resource", &resources.UnsupportedResourceError{Selector: "dashboards", Reason: "not served"}, gcxerrors.SummaryEndpointNotAvailable, gcxerrors.ExitGeneralError},
		{"invalid selector", resources.InvalidSelectorError{Command: "a////", Err: "too many segments"}, gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"stack creation timeout", &cloud.StackCreationTimeoutError{Slug: "mystack", Err: context.DeadlineExceeded}, gcxerrors.SummaryNetworkError, gcxerrors.ExitGeneralError},
		{"basic auth 401", &login.BasicAuthCheckError{Status: 401}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"basic auth 403", &login.BasicAuthCheckError{Status: 403}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"basic auth anonymous", &login.BasicAuthCheckError{}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"basic auth transport", &login.BasicAuthCheckError{Cause: errors.New("tls")}, gcxerrors.SummaryNetworkError, gcxerrors.ExitGeneralError},
		{"basic auth 500", &login.BasicAuthCheckError{Status: 500}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"login GCOM 403", &login.GCOMStackError{Slug: "mystack", Status: 403, Cause: errors.New("forbidden")}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"login GCOM 401", &login.GCOMStackError{Slug: "mystack", Status: 401, Cause: errors.New("unauthorized")}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"login GCOM 404", &login.GCOMStackError{Slug: "mystack", Status: 404, Cause: errors.New("not found")}, gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{"login GCOM 500", &login.GCOMStackError{Slug: "mystack", Status: 500, Cause: errors.New("boom")}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"login health 401", &login.HealthCheckError{Server: "https://example.grafana.net", Status: 401, Cause: errors.New("unauthorized")}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"login health 403", &login.HealthCheckError{Server: "https://example.grafana.net", Status: 403, Cause: errors.New("forbidden")}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"login health unreachable", &login.HealthCheckError{Server: "https://example.grafana.net", Cause: errors.New("connection refused")}, gcxerrors.SummaryNetworkError, gcxerrors.ExitGeneralError},
		{"login k8s discovery", &login.K8sDiscoveryError{Server: "https://example.grafana.net", Cause: errors.New("not found")}, gcxerrors.SummaryEndpointNotAvailable, gcxerrors.ExitGeneralError},
		{"login version check", &login.VersionCheckError{Cause: &grafana.VersionIncompatibleError{Version: grafana11}}, gcxerrors.SummaryUnsupportedGrafanaVersion, gcxerrors.ExitVersionIncompatible},
		{"version incompatible", &grafana.VersionIncompatibleError{Version: grafana11}, gcxerrors.SummaryUnsupportedGrafanaVersion, gcxerrors.ExitVersionIncompatible},
		{"required flags", errors.New(`required flag(s) "datasource" not set`), gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"SM credentials missing", errors.New("SM token not configured: no cloud config: context has no cloud auth"), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"SM URL not configured", errors.New("SM URL not configured: no server"), gcxerrors.SummaryInvalidConfiguration, gcxerrors.ExitGeneralError},
		{"SM register/install denied", errors.New("SM token not configured: SM register/install: request failed with status 403: forbidden"), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"SM token catch-all", errors.New("SM token not configured: SM register/install: request failed with status 503: unavailable"), gcxerrors.SummaryInvalidConfiguration, gcxerrors.ExitGeneralError},
		{"cloud credentials missing", errors.New("context has no cloud auth: run gcx cloud login"), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"cloud stack missing", errors.New("cloud stack is not configured: set the slug"), gcxerrors.SummaryInvalidConfiguration, gcxerrors.ExitGeneralError},
		{"adaptive traces scope", errors.New("adaptive-traces: list policies: unexpected status 401: invalid scope requested"), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"adaptive metrics scope", errors.New("adaptive-metrics: list rules: status 401: invalid scope requested"), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"stack info 403", errors.New(`k6: failed to get stack info for "mystack": status 403: forbidden`), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"fleet plugin missing", &fleet.HTTPError{Status: 404, Path: "/foo", Body: `{"message":"plugin route match not found"}`}, gcxerrors.SummaryEndpointNotAvailable, gcxerrors.ExitGeneralError},
		{"fleet 401", &fleet.HTTPError{Status: 401, Path: "/foo"}, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"fleet 403", &fleet.HTTPError{Status: 403, Path: "/foo"}, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"instrumentation mutually exclusive flags", fmt.Errorf("--a and --b: %w", instrumentation.ErrMutuallyExclusiveFlags), gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"instrumentation conflict", rmw.ConflictError{Diff: "tracing: true -> false"}, gcxerrors.SummaryResourceConflict, gcxerrors.ExitGeneralError},
		{"unknown --json field", cmdoutput.UnknownFieldSelectionError{Fields: []string{"bogus"}}, gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"jq runtime", cmdoutput.JQRuntimeError{Err: errors.New("cannot iterate"), Shape: "a number"}, gcxerrors.SummaryInvalidCommandUsage, gcxerrors.ExitUsageError},
		{"stack delete protection", fmt.Errorf("failed to delete stack: %w", &cloud.GCOMHTTPError{Status: 409, Message: "instance is protected"}), gcxerrors.SummaryResourceConflict, gcxerrors.ExitGeneralError},
		{"stack invalid argument", fmt.Errorf("failed to create stack: %w", &cloud.GCOMHTTPError{Status: 409, Code: "InvalidArgument", Message: "Invalid region"}), gcxerrors.SummaryInvalidStackRequest, gcxerrors.ExitUsageError},
		{"stack conflict", fmt.Errorf("failed to update stack: %w", &cloud.GCOMHTTPError{Status: 409, Code: "Conflict", Message: "conflict"}), gcxerrors.SummaryResourceConflict, gcxerrors.ExitGeneralError},
		{"stacks 403", fmt.Errorf("failed to list stacks: %w", &cloud.GCOMHTTPError{Status: 403, Message: "denied"}), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"stacks 401", fmt.Errorf("failed to list stacks: %w", &cloud.GCOMHTTPError{Status: 401, Message: "bad token"}), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"partial failure", gcxerrors.NewPartialFailureError("push", 100, 10), gcxerrors.SummaryPartialFailure, gcxerrors.ExitPartialFailure},
		{"OAuth exchange 429", &auth.ExchangeStatusError{StatusCode: 429}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"OAuth exchange 503", &auth.ExchangeStatusError{StatusCode: 503}, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{"browser cancelled", fmt.Errorf("OAuth flow failed: %w", auth.ErrBrowserCancelled), gcxerrors.SummaryOperationCancelled, gcxerrors.ExitCancelled},
		{"context cancelled", fmt.Errorf("op: %w", context.Canceled), gcxerrors.SummaryOperationCancelled, gcxerrors.ExitCancelled},
		{"cloud orgs needs OAuth", fmt.Errorf("failed to list cloud organisations: %w", cloud.ErrUserOAuthRequired), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"cloud orgs 401", fmt.Errorf("failed to list cloud organisations: %w", &cloud.GCOMHTTPError{Status: 401}), gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{"cloud orgs 403", fmt.Errorf("failed to list cloud organisations: %w", &cloud.GCOMHTTPError{Status: 403}), gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{"signup incomplete", &login.SignupIncompleteError{Err: fmt.Errorf("OAuth flow failed: %w", auth.ErrBrowserCancelled), Recovery: "gcx login"}, gcxerrors.SummaryOperationCancelled, gcxerrors.ExitCancelled},
		{"fallback plain", errors.New("something broke"), gcxerrors.SummaryUnexpectedError, gcxerrors.ExitGeneralError},
		{"fallback wrapped", fmt.Errorf("failed to create client: %w", errors.New("boom")), gcxerrors.SummaryUnexpectedError, gcxerrors.ExitGeneralError},
		{"fallback provider tag", errors.New("k6: boom"), gcxerrors.SummaryUnexpectedError, gcxerrors.ExitGeneralError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fail.ErrorToDetailedError(tc.err)
			require.NotNil(t, got)
			exit := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				exit = *got.ExitCode
			}
			assert.Equal(t, tc.wantExit, exit)
			assert.Equal(t, tc.wantSummary, got.Summary)
			assert.Contains(t, gcxerrors.Summaries(), got.Summary, "summary is not in the vocabulary")
		})
	}
}
