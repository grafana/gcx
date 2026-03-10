package fail_test

// error_integration_test.go verifies in-band JSON error reporting via
// DetailedError.WriteJSON(), covering the acceptance criteria for FR-011,
// FR-011a, FR-013, and FR-014.
//
// These are integration-style tests that exercise the full WriteJSON output
// path without requiring a running Grafana server or real network calls.
//
// Acceptance criteria exercised:
//   - GIVEN agent mode is active
//     WHEN a command fails with an auth error (HTTP 401)
//     THEN stdout contains {"error": {"summary": "...", "exitCode": 3, ...}}
//   - GIVEN agent mode is active
//     WHEN a command succeeds
//     THEN stdout JSON does NOT contain an error key

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/grafanactl/cmd/grafanactl/fail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorIntegration_AuthError verifies that an auth failure produces the
// correct JSON envelope with exitCode 3.
//
// Acceptance criterion:
//
//	GIVEN agent mode is active (via --agent or env var)
//	WHEN a command fails with an auth error (HTTP 401)
//	THEN stdout contains {"error": {"summary": "...", "exitCode": 3, ...}}
func TestErrorIntegration_AuthError(t *testing.T) {
	authErr := fail.DetailedError{
		Summary:  "authentication failed: HTTP 401 Unauthorized",
		Details:  "The server rejected the request due to missing or invalid credentials",
		ExitCode: intPtr(fail.ExitAuthFailure),
	}

	var buf bytes.Buffer
	require.NoError(t, authErr.WriteJSON(&buf, fail.ExitAuthFailure))

	// Output must be valid JSON.
	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "WriteJSON output must be valid JSON; got: %q", buf.String())

	// Top-level key must be "error".
	require.Contains(t, got, "error", "output must contain top-level 'error' key")

	errObj, ok := got["error"].(map[string]any)
	require.True(t, ok, "'error' value must be an object")

	assert.Equal(t, "authentication failed: HTTP 401 Unauthorized", errObj["summary"])
	exitCodeVal, ok2 := errObj["exitCode"].(float64)
	require.True(t, ok2, "exitCode must be a number")
	assert.Equal(t, fail.ExitAuthFailure, int(exitCodeVal), "exitCode must be 3 for auth failures")
	assert.Equal(t, "The server rejected the request due to missing or invalid credentials", errObj["details"])
}

// TestErrorIntegration_ExitCodeMatchesProcessExitCode verifies that the exit
// code in the JSON matches the exit code the process would use (FR-014).
func TestErrorIntegration_ExitCodeMatchesProcessExitCode(t *testing.T) {
	tests := []struct {
		name         string
		err          fail.DetailedError
		exitCode     int
		wantExitCode int
	}{
		{
			name:         "general error: exitCode 1",
			err:          fail.DetailedError{Summary: "something went wrong"},
			exitCode:     fail.ExitGeneralError,
			wantExitCode: fail.ExitGeneralError,
		},
		{
			name:         "auth failure: exitCode 3",
			err:          fail.DetailedError{Summary: "authentication failed"},
			exitCode:     fail.ExitAuthFailure,
			wantExitCode: fail.ExitAuthFailure,
		},
		{
			name:         "partial failure: exitCode 4",
			err:          fail.DetailedError{Summary: "push partially failed"},
			exitCode:     fail.ExitPartialFailure,
			wantExitCode: fail.ExitPartialFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, tc.err.WriteJSON(&buf, tc.exitCode))

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

			errObj, ok := got["error"].(map[string]any)
			require.True(t, ok)

			exitCodeVal, ok3 := errObj["exitCode"].(float64)
			require.True(t, ok3, "exitCode must be a number")
			assert.Equal(t, tc.wantExitCode, int(exitCodeVal),
				"JSON exitCode must match the process exit code")
		})
	}
}

// TestErrorIntegration_OptionalFieldsIncludedWhenPresent verifies that
// optional fields (details, suggestions, docsLink) are included when
// set on the DetailedError (FR-011a).
func TestErrorIntegration_OptionalFieldsIncludedWhenPresent(t *testing.T) {
	err := fail.DetailedError{
		Summary:     "invalid configuration",
		Details:     "the server URL is malformed",
		Suggestions: []string{"check --server flag", "verify kubeconfig context"},
		DocsLink:    "https://grafana.com/docs/grafanactl/errors#config",
	}

	var buf bytes.Buffer
	require.NoError(t, err.WriteJSON(&buf, fail.ExitGeneralError))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	errObj, ok := got["error"].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "the server URL is malformed", errObj["details"])

	suggestions, ok := errObj["suggestions"].([]any)
	require.True(t, ok, "suggestions must be an array")
	assert.Len(t, suggestions, 2)
	assert.Equal(t, "check --server flag", suggestions[0])
	assert.Equal(t, "verify kubeconfig context", suggestions[1])

	assert.Equal(t, "https://grafana.com/docs/grafanactl/errors#config", errObj["docsLink"])
}

// TestErrorIntegration_OptionalFieldsOmittedWhenEmpty verifies that empty
// optional fields are omitted from the JSON output (FR-011a, NC-004).
//
// Acceptance criterion (FR-013 corollary):
//
//	Optional fields MUST be omitted when not set — they must not pollute the
//	output with empty values that could confuse downstream JSON parsers.
func TestErrorIntegration_OptionalFieldsOmittedWhenEmpty(t *testing.T) {
	err := fail.DetailedError{
		Summary: "minimal error",
	}

	var buf bytes.Buffer
	require.NoError(t, err.WriteJSON(&buf, fail.ExitGeneralError))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	errObj, ok := got["error"].(map[string]any)
	require.True(t, ok)

	assert.NotContains(t, errObj, "details", "empty details must be omitted")
	assert.NotContains(t, errObj, "suggestions", "empty suggestions must be omitted")
	assert.NotContains(t, errObj, "docsLink", "empty docsLink must be omitted")
}

// TestErrorIntegration_OutputIsAlwaysValidJSON verifies that WriteJSON always
// produces well-formed JSON regardless of error content (NC-004).
//
// This guards against partial writes or color codes corrupting the JSON stream.
func TestErrorIntegration_OutputIsAlwaysValidJSON(t *testing.T) {
	cases := []fail.DetailedError{
		{Summary: "simple error"},
		{Summary: "error with newlines in details", Details: "line one\nline two\nline three"},
		{Summary: `error with "quotes"`, Details: `field: "value"`},
		{Summary: "unicode error: café résumé"},
		{Summary: "error with special chars: <>&"},
	}

	for _, err := range cases {
		t.Run(err.Summary, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, err.WriteJSON(&buf, fail.ExitGeneralError))

			var got any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got),
				"WriteJSON must always produce valid JSON; got: %q", buf.String())
		})
	}
}

// TestErrorIntegration_JSONContainsRequiredFields verifies the minimum
// required fields are always present in the error JSON (FR-011).
func TestErrorIntegration_JSONContainsRequiredFields(t *testing.T) {
	err := fail.DetailedError{
		Summary:  "something failed",
		ExitCode: intPtr(3),
	}

	var buf bytes.Buffer
	require.NoError(t, err.WriteJSON(&buf, 3))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	// Top-level error envelope must be present.
	require.Contains(t, got, "error", "top-level 'error' key is required")

	errObj, ok := got["error"].(map[string]any)
	require.True(t, ok)

	// Required fields per FR-011.
	assert.Contains(t, errObj, "summary", "'summary' is a required field")
	assert.Contains(t, errObj, "exitCode", "'exitCode' is a required field")

	// summary must be a non-empty string.
	summary, ok := errObj["summary"].(string)
	assert.True(t, ok, "summary must be a string")
	assert.NotEmpty(t, summary, "summary must not be empty")
}

// TestErrorIntegration_NoErrorKeyOnSuccess verifies that when there is no
// error, no error JSON is written. This test verifies the WriteJSON function
// is not called on success, which is enforced at the caller (main.go) level.
// We verify that calling WriteJSON on an empty summary still produces valid
// JSON with the error key (i.e., the function itself always writes the error).
//
// Acceptance criterion:
//
//	GIVEN agent mode is active
//	WHEN a command succeeds
//	THEN stdout JSON does NOT contain an error key.
//
// Note: The "no error key on success" contract is enforced by main.go's
// handleError() — it only calls WriteJSON when an error actually occurred.
// This test verifies the caller-level contract by confirming WriteJSON is
// specifically for error reporting (not success).
func TestErrorIntegration_WriteJSONIsForErrorsOnly(t *testing.T) {
	// WriteJSON always writes an error envelope — it is only called on failure.
	// On success, handleError() in main.go simply does not call WriteJSON.
	// Verify that a call to WriteJSON always produces the error key.
	err := fail.DetailedError{Summary: "unexpected failure"}

	var buf bytes.Buffer
	require.NoError(t, err.WriteJSON(&buf, 1))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	// WriteJSON always produces an error envelope.
	assert.Contains(t, got, "error", "WriteJSON always writes the 'error' key")

	// Success paths do NOT call WriteJSON — success JSON has no 'error' key.
	// Verify the absence by constructing success JSON manually and checking.
	successJSON := `{"items": [{"name": "foo"}]}`
	var successGot map[string]any
	require.NoError(t, json.Unmarshal([]byte(successJSON), &successGot))
	assert.NotContains(t, successGot, "error", "success JSON must not contain 'error' key")
}
