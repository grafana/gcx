package queryerror

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxMessageLength = 4096

// APIError is a typed error for datasource query endpoints.
//
// It preserves the datasource type, operation, HTTP status, and the human
// message extracted from Grafana or downstream datasource responses so the fail
// package can render a useful error instead of dumping raw JSON.
type APIError struct {
	Datasource  string
	Operation   string
	StatusCode  int
	Message     string
	ErrorSource string
}

// New constructs an APIError with sanitized message fields.
func New(datasource, operation string, statusCode int, message, errorSource string) *APIError {
	return &APIError{
		Datasource:  strings.TrimSpace(datasource),
		Operation:   strings.TrimSpace(operation),
		StatusCode:  statusCode,
		Message:     sanitizeMessage(message),
		ErrorSource: strings.TrimSpace(errorSource),
	}
}

// FromBody constructs an APIError by extracting the most useful message from an
// HTTP response body. It understands Grafana's datasource query envelope as
// well as common {"error": ...} / {"message": ...} JSON responses.
//
// The embedded `results.*.status` from the Grafana query envelope is only used
// as a fallback when the transport status is 2xx — i.e., to surface query-level
// failures hiding inside a 200 OK. When the transport status is already an
// error (4xx/5xx), it is kept as the authoritative signal so auth, proxy, and
// gateway failures are not misclassified by downstream-supplied status codes.
func FromBody(datasource, operation string, statusCode int, body []byte) *APIError {
	message, errorSource, parsedStatus := extractMessage(body)
	if parsedStatus != 0 && statusCode >= 200 && statusCode < 300 {
		statusCode = parsedStatus
	}
	return New(datasource, operation, statusCode, message, errorSource)
}

func (e *APIError) Error() string {
	subject := strings.TrimSpace(strings.Join([]string{e.Datasource, e.Operation}, " "))
	if subject == "" {
		subject = "request"
	}

	if e.Message != "" {
		return fmt.Sprintf("%s failed with status %d: %s", subject, e.StatusCode, e.Message)
	}

	return fmt.Sprintf("%s failed with status %d", subject, e.StatusCode)
}

// IsParseError reports whether the datasource returned a syntax/parse error.
func (e *APIError) IsParseError() bool {
	msg := strings.ToLower(e.Message)
	return strings.Contains(msg, "parse error") || strings.Contains(msg, "syntax error")
}

type grafanaQueryEnvelope struct {
	Results map[string]grafanaQueryResult `json:"results"`
}

type grafanaQueryResult struct {
	Error       string `json:"error"`
	ErrorSource string `json:"errorSource,omitempty"`
	Status      int    `json:"status,omitempty"`
}

type commonErrorEnvelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details"`
}

func extractMessage(body []byte) (string, string, int) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", "", 0
	}

	var grafanaResp grafanaQueryEnvelope
	if err := json.Unmarshal(body, &grafanaResp); err == nil {
		if result, ok := grafanaResp.Results["A"]; ok && strings.TrimSpace(result.Error) != "" {
			return result.Error, result.ErrorSource, result.Status
		}
		for _, result := range grafanaResp.Results {
			if strings.TrimSpace(result.Error) != "" {
				return result.Error, result.ErrorSource, result.Status
			}
		}
	}

	var commonResp commonErrorEnvelope
	if err := json.Unmarshal(body, &commonResp); err == nil {
		switch {
		case strings.TrimSpace(commonResp.Error) != "":
			return commonResp.Error, "", 0
		case strings.TrimSpace(commonResp.Message) != "":
			return commonResp.Message, "", 0
		case strings.TrimSpace(commonResp.Details) != "":
			return commonResp.Details, "", 0
		}
	}

	return sanitizeMessage(trimmed), "", 0
}

func sanitizeMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}

	if len(message) <= maxMessageLength {
		return message
	}

	return strings.TrimSpace(message[:maxMessageLength]) + "…"
}
