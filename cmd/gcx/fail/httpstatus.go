package fail

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// convertHTTPStatusErrors is the last typed converter: domain converters get
// first choice of remediation. Only the concrete transport error is matched.
func convertHTTPStatusErrors(err error) (*gcxerrors.DetailedError, bool) {
	var statusErr *gcxerrors.HTTPStatusError
	if !errors.As(err, &statusErr) {
		return nil, false
	}
	detailed := &gcxerrors.DetailedError{Summary: httpStatusSummary(statusErr.Status)}
	if statusErr.ServerMessage == "" {
		// Raw API passthrough and migrated helpers retain their complete text.
		detailed.Details = err.Error()
	} else {
		operation := wrappedTypedErrorContext(err, statusErr)
		detailed.Details = httpFailureDetails(operation, statusErr.ServerMessage, statusErr.Status, statusErr.TraceID)
	}
	switch statusErr.Status {
	case http.StatusUnauthorized:
		detailed.ExitCode = new(gcxerrors.ExitAuthFailure)
		detailed.Suggestions = []string{reauthSuggestion, "Verify the configured token is valid and has not expired"}
	case http.StatusForbidden:
		detailed.ExitCode = new(gcxerrors.ExitAuthFailure)
		detailed.Suggestions = []string{"Check your roles and access-policy scopes", "Check your permissions: gcx setup status"}
	}
	return detailed, true
}

func httpStatusSummary(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return gcxerrors.SummaryAuthenticationFailed
	case http.StatusForbidden:
		return gcxerrors.SummaryAuthorizationFailed
	case http.StatusNotFound:
		return gcxerrors.SummaryResourceNotFound
	case http.StatusConflict:
		return gcxerrors.SummaryResourceConflict
	default:
		return gcxerrors.SummaryAPIError
	}
}

func httpFailureDetails(operation, message string, status int, traceID string) string {
	metadata := fmt.Sprintf("HTTP %d", status)
	if traceID != "" {
		metadata += ", trace ID " + traceID
	}
	if operation != "" {
		message = strings.TrimSpace(operation) + ": " + message
	}
	return message + " (" + metadata + ")"
}

// Discovery wrappers must not turn service outages into missing credentials.
// Reuse ordinary conversion on the cause after giving prerequisite/scope
// handling its first choice. The complete wrapper stays only in the details.
func convertSMDiscoveryErrors(err error) (*gcxerrors.DetailedError, bool) {
	if !strings.Contains(err.Error(), "SM token not configured") {
		return nil, false
	}
	if detailed, matched := convertSMConfigErrors(err); matched && detailed.Summary != gcxerrors.SummaryAPIError {
		detailed.Details = "SM token auto-discovery failed: " + err.Error()
		detailed.Parent = nil
		return detailed, true
	}
	cause := errors.Unwrap(err)
	for cause != nil && strings.Contains(cause.Error(), "SM token not configured") {
		cause = errors.Unwrap(cause)
	}
	if cause == nil {
		return convertSMConfigErrors(err)
	}
	detailed := ErrorToDetailedError(cause)
	if detailed == nil || detailed.Summary == gcxerrors.SummaryUnexpectedError {
		return convertSMConfigErrors(err)
	}
	if detailed.Summary == gcxerrors.SummaryInvalidConfiguration {
		detailed.Summary = gcxerrors.SummaryAPIError
	}
	text := detailed.Details
	if detailed.Parent != nil {
		text = joinErrorDetails(text, detailed.Parent.Error())
	}
	detailed.Details = "SM token auto-discovery failed: " + text
	detailed.Parent = nil
	return detailed, true
}
