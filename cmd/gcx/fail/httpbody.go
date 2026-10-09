package fail

import "encoding/json"

// responseErrorFields extracts conventional API fields without discarding a
// raw response that is not a JSON error object.
func responseErrorFields(body string) (string, string, string) {
	var parsed struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		TraceID string `json:"traceID"`
	}
	_ = json.Unmarshal([]byte(body), &parsed)
	message := parsed.Message
	if message == "" {
		return body, "", ""
	}
	return message, parsed.Code, parsed.TraceID
}

func errorMessageWithCode(message, code string) string {
	if code != "" {
		return message + " (code " + code + ")"
	}
	return message
}
