package fail_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHTMLResponsesKeepStatusClassification(t *testing.T) {
	const body = "<!DOCTYPE html><html><body>secret login page</body></html>"
	for _, statusCode := range []int32{401, 403, 404, 409, 429, 500, 502, 503, 504} {
		status := int(statusCode)
		constructors := map[string]error{
			"shared":     providers.FormatError(status, []byte(body)),
			"fleet":      &fleet.HTTPError{Status: status, Path: "/GetPipeline", Body: body},
			"gcom":       fmt.Errorf("failed to list stacks: %w", &cloud.GCOMHTTPError{Status: status, Body: body}),
			"datasource": datasources.NewAPIError("list datasources", "", status, []byte(body)),
			"query":      queryerror.FromBody("loki", "query", status, []byte(body)),
			"service":    fakeServiceAPIError{statusCode: status, service: "Knowledge Graph", message: body},
			"dynamic":    &k8sapi.StatusError{ErrStatus: metav1.Status{Code: statusCode, Reason: metav1.StatusReasonUnknown, Message: "opaque server response", Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseTypeUnexpectedServerResponse, Message: body}}}}},
		}
		for name, err := range constructors {
			t.Run(fmt.Sprintf("%s/%d", name, status), func(t *testing.T) {
				got := toDetailedError(t, fmt.Errorf("operation context: %w", err))
				summary := gcxerrors.SummaryAPIError
				exit := gcxerrors.ExitGeneralError
				switch status {
				case 401:
					summary = gcxerrors.SummaryAuthenticationFailed
					exit = 3
				case 403:
					summary = gcxerrors.SummaryAuthorizationFailed
					exit = 3
				case 404:
					summary = gcxerrors.SummaryResourceNotFound
				case 409:
					summary = gcxerrors.SummaryResourceConflict
				}
				if status == 409 && (name == "query" || name == "datasource" || name == "service") {
					summary = gcxerrors.SummaryAPIError
				}
				if name == "fleet" && status == 404 {
					summary = gcxerrors.SummaryEndpointNotAvailable
				}
				actual := gcxerrors.ExitGeneralError
				if got.ExitCode != nil {
					actual = *got.ExitCode
				}
				assert.Equal(t, exit, actual)
				assert.Equal(t, summary, got.Summary)
				assert.Contains(t, got.Details, "got an HTML page")
				assert.Contains(t, got.Details, fmt.Sprintf("HTTP %d %s", status, http.StatusText(status)))
				assert.Contains(t, got.Details, "operation context")
				assert.NotContains(t, got.Error(), "secret login page")
				assert.NotContains(t, got.Error(), "<html>")
				assert.NoError(t, got.Parent)
			})
		}
	}
}

func TestSharedHTTPBodyVariants(t *testing.T) {
	for _, body := range []string{"", "Internal Server Error", `{"reason":"upstream failed"}`, `{"message":"upstream failed","traceID":"t1"}`, `{"message":`, "plain text error"} {
		t.Run(body, func(t *testing.T) {
			got := toDetailedError(t, fmt.Errorf("caller prefix: %w; caller suffix", providers.FormatError(502, []byte(body))))
			assert.Equal(t, gcxerrors.SummaryAPIError, got.Summary)
			assert.Contains(t, got.Details, "caller prefix")
			assert.Contains(t, got.Details, "caller suffix")
			assert.NotContains(t, got.Details, "got an HTML page")
		})
	}
}

func TestHTMLContentTypeAndExemptions(t *testing.T) {
	for name, err := range map[string]error{
		"shared": &gcxerrors.HTTPStatusError{Status: 403, Message: "unchanged opaque message", ServerMessage: "opaque", ContentType: "text/html; charset=utf-8"},
		"fleet":  &fleet.HTTPError{Status: 502, Body: "opaque", ContentType: "text/html"},
		"gcom":   fmt.Errorf("failed to list stacks: %w", &cloud.GCOMHTTPError{Status: 502, Body: "opaque", ContentType: "application/xhtml+xml"}),
	} {
		t.Run(name, func(t *testing.T) {
			got := toDetailedError(t, err)
			assert.Contains(t, got.Details, "got an HTML page")
			assert.NotContains(t, got.Error(), "opaque")
		})
	}
	embedded := queryerror.FromBody("prometheus", "query", 200, []byte(`{"results":{"A":{"status":400,"error":"<html>query expression</html>"}}}`))
	got := toDetailedError(t, embedded)
	assert.Equal(t, gcxerrors.SummaryInvalidQuery, got.Summary)
	assert.Contains(t, got.Details, "<html>query expression</html>")
	assert.Nil(t, got.ExitCode)
	scopePage := fakeServiceAPIError{statusCode: 401, service: "Adaptive Logs", message: "<html><body>invalid scope requested</body></html>"}
	got = toDetailedError(t, scopePage)
	assert.Equal(t, gcxerrors.SummaryAuthenticationFailed, got.Summary)
	assert.NotContains(t, got.Error(), "invalid scope requested")
}

func TestNonHTMLMessagesAreNotTruncatedByRendering(t *testing.T) {
	body := strings.Repeat("server detail ", 1000)
	for name, err := range map[string]error{
		"shared": providers.FormatError(502, []byte(body)),
		"fleet":  &fleet.HTTPError{Status: 502, Body: body},
		"gcom":   fmt.Errorf("failed to list stacks: %w", &cloud.GCOMHTTPError{Status: 502, Body: body}),
	} {
		t.Run(name, func(t *testing.T) {
			got := toDetailedError(t, err)
			assert.Contains(t, got.Details, body)
		})
	}
}

func TestSMDiscoveryHTMLPermissionFailure(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			err := fmt.Errorf("failed to load SM config: %w", fmt.Errorf("SM token not configured: register/install API failed: %w", providers.FormatError(status, []byte("<html><body>private scope page</body></html>"))))
			got := toDetailedError(t, err)
			assert.Equal(t, gcxerrors.SummaryAuthorizationFailed, got.Summary)
			require.NotNil(t, got.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
			assert.Contains(t, got.Details, "SM token not configured")
			assert.Contains(t, got.Details, "got an HTML page")
			assert.NotContains(t, got.Error(), "private scope page")
			assert.NoError(t, got.Parent)
		})
	}
}
