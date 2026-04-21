package datasources

import (
	"encoding/json"
	"fmt"
	"strings"
)

// APIError is a typed error returned by the Grafana datasource REST API.
type APIError struct {
	Operation  string
	Identifier string
	StatusCode int
	Message    string
}

func NewAPIError(operation, identifier string, statusCode int, body []byte) *APIError {
	return &APIError{
		Operation:  strings.TrimSpace(operation),
		Identifier: strings.TrimSpace(identifier),
		StatusCode: statusCode,
		Message:    extractAPIErrorMessage(body),
	}
}

func (e *APIError) Error() string {
	target := strings.TrimSpace(strings.Join([]string{e.Operation, quotedIdentifier(e.Identifier)}, " "))
	if target == "" {
		target = "datasource API request"
	}
	if e.Message != "" {
		return fmt.Sprintf("%s failed with status %d: %s", target, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("%s failed with status %d", target, e.StatusCode)
}

func (e *APIError) HTTPStatusCode() int {
	return e.StatusCode
}

func (e *APIError) APIServiceName() string {
	return "Datasources"
}

func (e *APIError) APIUserMessage() string {
	return e.Message
}

func extractAPIErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		switch {
		case strings.TrimSpace(payload.Message) != "":
			return strings.TrimSpace(payload.Message)
		case strings.TrimSpace(payload.Error) != "":
			return strings.TrimSpace(payload.Error)
		}
	}

	return trimmed
}

func quotedIdentifier(identifier string) string {
	if identifier == "" {
		return ""
	}
	return fmt.Sprintf("%q", identifier)
}
