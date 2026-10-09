package fail_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetStatusAndDetails(t *testing.T) {
	for _, tc := range []struct {
		status  int
		summary string
		exit    int
	}{
		{401, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{403, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{404, gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{409, gcxerrors.SummaryResourceConflict, gcxerrors.ExitGeneralError},
		{429, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{500, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{502, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{503, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{504, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			httpErr := &fleet.HTTPError{Status: tc.status, Path: "/GetPipeline", Body: `{"code":"not_found","message":"specific server failure","traceID":"trace-1"}`}
			got := toDetailedError(t, fmt.Errorf("fleet: get pipeline pipeline-1: %w", httpErr))
			assert.Equal(t, tc.summary, got.Summary)
			exit := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				exit = *got.ExitCode
			}
			assert.Equal(t, tc.exit, exit)
			require.NoError(t, got.Parent)
			assert.Equal(t, fmt.Sprintf("fleet: get pipeline pipeline-1 [/GetPipeline]: specific server failure (code not_found) (HTTP %d, trace ID trace-1)", tc.status), got.Details)
			assert.Equal(t, 1, strings.Count(got.Details, "specific server failure"))
		})
	}
}

func TestFleetEndpointNotFoundBodies(t *testing.T) {
	for _, body := range []string{"", "404 page not found", `{"message":"resource not found"}`, `{"message":"Plugin not found"}`, `{"message":"plugin is not enabled"}`, `{"message":"plugin route match not found"}`, `{"code":"not_found"`, `{"code":"unimplemented"}`} {
		t.Run(body, func(t *testing.T) {
			got := toDetailedError(t, &fleet.HTTPError{Status: http.StatusNotFound, Path: "/UnknownRPC", Body: body})
			assert.Equal(t, gcxerrors.SummaryEndpointNotAvailable, got.Summary)
			assert.Nil(t, got.ExitCode)
			require.NoError(t, got.Parent)
			message := body
			var parsed struct {
				Message string `json:"message"`
			}
			if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Message != "" {
				message = parsed.Message
			}
			assert.Contains(t, got.Details, message)
			assert.Contains(t, got.Details, "HTTP 404")
		})
	}
}

func TestGCOMDetailsOnce(t *testing.T) {
	for _, tc := range []struct {
		status  int
		summary string
		exit    int
	}{
		{401, gcxerrors.SummaryAuthenticationFailed, gcxerrors.ExitAuthFailure},
		{403, gcxerrors.SummaryAuthorizationFailed, gcxerrors.ExitAuthFailure},
		{404, gcxerrors.SummaryResourceNotFound, gcxerrors.ExitGeneralError},
		{409, gcxerrors.SummaryResourceConflict, gcxerrors.ExitGeneralError},
		{429, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{500, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{502, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{503, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
		{504, gcxerrors.SummaryAPIError, gcxerrors.ExitGeneralError},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			httpErr := &cloud.GCOMHTTPError{Status: tc.status, Code: "ServerCode", Message: "specific server failure", Body: `{"code":"ServerCode","message":"specific server failure","traceID":"trace-2"}`}
			got := toDetailedError(t, fmt.Errorf("failed to get stack stack-1: %w", httpErr))
			assert.Equal(t, tc.summary, got.Summary)
			exit := gcxerrors.ExitGeneralError
			if got.ExitCode != nil {
				exit = *got.ExitCode
			}
			assert.Equal(t, tc.exit, exit)
			require.NoError(t, got.Parent)
			assert.Equal(t, fmt.Sprintf("failed to get stack stack-1: specific server failure (code ServerCode) (HTTP %d, trace ID trace-2)", tc.status), got.Details)
		})
	}
}

func TestGCOMRawBodyFallback(t *testing.T) {
	for _, body := range []string{"", "server unavailable", `{"reason":"down"}`, `{"code":"Unavailable","traceID":"trace-raw"}`, `{"message":`, strings.Repeat("long response ", 200)} {
		t.Run(body, func(t *testing.T) {
			got := toDetailedError(t, fmt.Errorf("failed to list stacks: %w", &cloud.GCOMHTTPError{Status: 503, Body: body}))
			assert.Equal(t, gcxerrors.SummaryAPIError, got.Summary)
			assert.Equal(t, "failed to list stacks: "+body+" (HTTP 503)", got.Details)
			require.NoError(t, got.Parent)
		})
	}
}

func TestFilesystemDetailsOnce(t *testing.T) {
	for _, tc := range []struct {
		cause   error
		summary string
	}{
		{os.ErrNotExist, gcxerrors.SummaryFileNotFound},
		{os.ErrInvalid, gcxerrors.SummaryInvalidPath},
		{os.ErrPermission, gcxerrors.SummaryFileAccessDenied},
	} {
		t.Run(tc.summary, func(t *testing.T) {
			err := fmt.Errorf("load input: %w", &fs.PathError{Op: "open", Path: "/input/config.json", Err: tc.cause})
			got := toDetailedError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.summary, got.Summary)
			assert.Equal(t, err.Error(), got.Details)
			assert.Equal(t, 1, strings.Count(got.Details, "/input/config.json"))
			require.NoError(t, got.Parent)
		})
	}
}
