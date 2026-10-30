package fail_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestSharedHTTPStatusClassification(t *testing.T) {
	for _, code := range []int{200, 401, 403, 404, 409, 429, 500, 502, 503, 504} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			err := fmt.Errorf("load resource: %w", providers.FormatError(code, []byte(`{"message":"server explanation","traceID":"trace-1"}`)))
			got := toDetailedError(t, err)
			summary := gcxerrors.SummaryAPIError
			exit := gcxerrors.ExitGeneralError
			switch code {
			case 401:
				summary = gcxerrors.SummaryAuthenticationFailed
				exit = gcxerrors.ExitAuthFailure
			case 403:
				summary = gcxerrors.SummaryAuthorizationFailed
				exit = gcxerrors.ExitAuthFailure
			case 404:
				summary = gcxerrors.SummaryResourceNotFound
			case 409:
				summary = gcxerrors.SummaryResourceConflict
			}
			assert.Equal(t, summary, got.Summary)
			actual := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				actual = *got.ExitCode
			}
			assert.Equal(t, exit, actual)
			assert.Contains(t, got.Details, "load resource")
			assert.Contains(t, got.Details, "server explanation")
			assert.NoError(t, got.Parent)
		})
	}
}

func TestRawHTTPStatusRetainsBody(t *testing.T) {
	body := "<html><body>login</body></html>"
	for _, status := range []int{401, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			text := fmt.Sprintf("HTTP %d: %s", status, body)
			got := toDetailedError(t, &gcxerrors.HTTPStatusError{Status: status, Message: text})
			summary := gcxerrors.SummaryAuthenticationFailed
			if status == 403 {
				summary = gcxerrors.SummaryAuthorizationFailed
			}
			assert.Equal(t, summary, got.Summary)
			assert.Equal(t, text, got.Details)
			assert.NotContains(t, got.Details, "expected an API response")
			assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
		})
	}
}

func TestDatasourceAuthenticationFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		message    string
		wantExit   int
		wantAdvice bool
	}{
		{"datasource credentials", 400, "Authentication to data source failed", gcxerrors.ExitAuthFailure, true},
		{"ordinary bad request", 400, "invalid rule group", gcxerrors.ExitGeneralError, false},
		{"unrelated message", 400, "Authentication to data source failed during a previous request", gcxerrors.ExitGeneralError, false},
		{"caller credentials", 401, "Authentication to data source failed", gcxerrors.ExitAuthFailure, false},
		{"server failure", 500, "Authentication to data source failed", gcxerrors.ExitGeneralError, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"message":%q,"traceID":"trace-1"}`, tc.message)
			err := fmt.Errorf("list datasource rules: %w", providers.FormatError(tc.status, []byte(body)))
			got := toDetailedError(t, err)
			exit := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				exit = *got.ExitCode
			}
			assert.Equal(t, tc.wantExit, exit)
			assert.Contains(t, got.Details, tc.message)
			assert.Contains(t, got.Details, fmt.Sprintf("HTTP %d", tc.status))
			assert.Contains(t, got.Details, "trace-1")
			if tc.wantAdvice {
				assert.Equal(t, gcxerrors.SummaryAuthenticationFailed, got.Summary)
				if assert.Len(t, got.Suggestions, 2) {
					assert.Contains(t, got.Suggestions[0], "gcx datasources health <uid>")
					assert.Contains(t, got.Suggestions[0], "gcx datasources list -o json")
					assert.Contains(t, got.Suggestions[1], "ruler configuration")
				}
			} else {
				for _, suggestion := range got.Suggestions {
					assert.NotContains(t, suggestion, "gcx datasources")
				}
			}
		})
	}
}

func TestRulerDatasourceAuthenticationResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/ruler/test-ds/api/v1/rules", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Authentication to data source failed"}`))
	}))
	defer server.Close()
	client, err := alert.NewRulerClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}, "test-ds", "mimir")
	require.NoError(t, err)
	_, err = client.ListNamespaces(context.Background())
	require.Error(t, err)
	detailed := toDetailedError(t, err)
	assert.Equal(t, gcxerrors.SummaryAuthenticationFailed, detailed.Summary)
	if assert.NotNil(t, detailed.ExitCode) {
		assert.Equal(t, gcxerrors.ExitAuthFailure, *detailed.ExitCode)
	}
	assert.Contains(t, detailed.Details, "HTTP 400")
}
