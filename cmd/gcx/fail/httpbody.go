package fail

import (
	"encoding/json"

	"github.com/grafana/gcx/internal/providers"
)

// responseErrorFields extracts conventional API fields without discarding a
// raw response that is not a JSON error object.
func responseErrorFields(body string) (string, string, string) {
	message, code, traceID := parsedResponseErrorFields(body)
	if message == "" {
		return body, "", ""
	}
	return message, code, traceID
}

// parsedResponseErrorFields never falls back to the raw body, so auth failures
// can retain a server message without exposing opaque or HTML responses.
func parsedResponseErrorFields(body string) (string, string, string) {
	var parsed struct {
		providers.ErrorResponse

		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", "", ""
	}
	message := parsed.ErrorMessage()
	if message == "" {
		message = parsed.Code
	}
	return message, parsed.Code, parsed.TraceID
}

func errorMessageWithCode(message, code string) string {
	if code != "" && code != message {
		return message + " (code " + code + ")"
	}
	return message
}
