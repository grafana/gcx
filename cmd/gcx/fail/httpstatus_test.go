package fail_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
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
	got := toDetailedError(t, &gcxerrors.HTTPStatusError{Status: 403, Message: "HTTP 403: " + body})
	assert.Equal(t, gcxerrors.SummaryAuthorizationFailed, got.Summary)
	assert.Contains(t, got.Details, body)
	assert.Equal(t, gcxerrors.ExitAuthFailure, *got.ExitCode)
}
