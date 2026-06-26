package smcfg

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HandleErrorResponse reads a non-2xx HTTP response body and returns a descriptive error.
// It attempts to decode the SM API's JSON error format ({error, msg}) before falling
// back to the raw body or a status-code-only message.
func HandleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}
	return HandleErrorBody(resp.StatusCode, body)
}

// HandleErrorBody builds a descriptive error from an already-read non-2xx status
// code and body. The dual-mode typed clients read proxy responses into memory
// (the datasource-proxy transport returns []byte, not an *http.Response), so they
// use this variant; HandleErrorResponse delegates here after reading the body.
func HandleErrorBody(statusCode int, body []byte) error {
	var errResp struct {
		Error string `json:"error"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Error != "" {
			return fmt.Errorf("request failed with status %d: %s", statusCode, errResp.Error)
		}
		if errResp.Msg != "" {
			return fmt.Errorf("request failed with status %d: %s", statusCode, errResp.Msg)
		}
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", statusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", statusCode)
}
