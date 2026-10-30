package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
)

// cmd/gcx/fail matches this name to suggest the adaptive-logs:admin scope.
const serviceName = "Logs Archive"

// Bucket-check codes. config_invalid comes with HTTP 400, the others with
// HTTP 403.
const (
	codeConfigInvalid          = "config_invalid"
	codeOwnershipMarkerMissing = "ownership_marker_missing"
	codeReadAccessDenied       = "read_access_denied"
	codeWriteAccessDenied      = "write_access_denied"
	codeDestinationUnreachable = "destination_unreachable"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// APIError implements the service API error interface that cmd/gcx/fail renders.
type APIError struct {
	StatusCode  int
	Message     string
	Code        string
	FieldErrors []FieldError
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("API request failed (HTTP %d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API request failed (HTTP %d)", e.StatusCode)
}

func (e *APIError) HTTPStatusCode() int { return e.StatusCode }

func (e *APIError) APIServiceName() string { return serviceName }

func (e *APIError) APIUserMessage() string { return e.Message }

func (e *APIError) codes() []string {
	var codes []string
	if e.Code != "" {
		codes = append(codes, e.Code)
	}
	for _, fe := range e.FieldErrors {
		if fe.Code != "" && !slices.Contains(codes, fe.Code) {
			codes = append(codes, fe.Code)
		}
	}
	return codes
}

func apiErrorFromResponseBody(statusCode int, body []byte) *APIError {
	var envelope struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Errors  []FieldError    `json:"errors"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		apiErr := &APIError{
			StatusCode:  statusCode,
			Message:     envelope.Message,
			Code:        envelope.Code,
			FieldErrors: envelope.Errors,
		}
		for _, raw := range []json.RawMessage{envelope.Result, envelope.Error} {
			var message string
			if apiErr.Message == "" && json.Unmarshal(raw, &message) == nil {
				apiErr.Message = message
			}
		}
		if apiErr.Message != "" || apiErr.Code != "" || len(apiErr.FieldErrors) > 0 {
			if apiErr.Message == "" {
				apiErr.Message = errorBodySummary(statusCode, body)
			}
			return apiErr
		}
	}

	return &APIError{StatusCode: statusCode, Message: errorBodySummary(statusCode, body)}
}

func errorBodySummary(statusCode int, body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ToValidUTF8(s, "\uFFFD")
	if s == "" {
		if text := http.StatusText(statusCode); text != "" {
			return text
		}
		return "empty response body"
	}
	const maxSummaryLen = 240
	if len(s) > maxSummaryLen {
		cut := maxSummaryLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return s[:cut] + "..."
	}
	return s
}

//nolint:gochecknoglobals // Static lookup table.
var destinationCheckSuggestions = map[string]string{
	codeConfigInvalid:          "Run 'gcx logs archive principal get' to see the cloud provider and region the bucket must use",
	codeOwnershipMarkerMissing: "Upload an empty object named grafana-archive-ownership under <path-prefix>/tenant=<logs-instance-id>/ in the bucket, then retry",
	codeReadAccessDenied:       "Grant the principal shown by 'gcx logs archive principal get' list, read and write access to the bucket",
	codeWriteAccessDenied:      "Grant the principal shown by 'gcx logs archive principal get' list, read and write access to the bucket",
	codeDestinationUnreachable: "Check that the bucket exists and that its name and region match 'gcx logs archive principal get'",
}

const unknownCheckSuggestion = "Check that the principal shown by 'gcx logs archive principal get' has list, read and write access to the bucket"

// A failed bucket check is a 403, which cmd/gcx/fail would report as a
// credential problem (exit 3). The API sends 403 only for bucket checks, always
// with field errors, so any 403 with field errors is treated as one.
func explainDestinationCheck(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}

	var suggestions []string
	for _, code := range apiErr.codes() {
		suggestion, ok := destinationCheckSuggestions[code]
		if ok && !slices.Contains(suggestions, suggestion) {
			suggestions = append(suggestions, suggestion)
		}
	}
	if len(suggestions) == 0 {
		if apiErr.StatusCode != http.StatusForbidden || len(apiErr.FieldErrors) == 0 {
			return err
		}
		suggestions = []string{unknownCheckSuggestion}
	}

	lines := []string{fmt.Sprintf("%s destination check failed (HTTP %d)", serviceName, apiErr.StatusCode)}
	if len(apiErr.FieldErrors) > 0 {
		for _, fe := range apiErr.FieldErrors {
			lines = append(lines, fmt.Sprintf("%s: %s", fe.Field, fe.Message))
		}
	} else if apiErr.Message != "" {
		lines = append(lines, apiErr.Message)
	}

	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryAPIError,
		Details:     strings.Join(lines, "\n"),
		Suggestions: suggestions,
		DocsLink:    docs.AdaptiveLogsArchive,
	}
}
