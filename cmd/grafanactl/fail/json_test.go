package fail_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/grafanactl/cmd/grafanactl/fail"
)

func intPtr(i int) *int {
	p := new(int)
	*p = i
	return p
}

func TestDetailedError_WriteJSON(t *testing.T) {
	tests := []struct {
		name     string
		err      fail.DetailedError
		exitCode int
		wantJSON map[string]any
	}{
		{
			name: "minimal error with summary and exitCode only",
			err: fail.DetailedError{
				Summary: "something went wrong",
			},
			exitCode: 1,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":  "something went wrong",
					"exitCode": float64(1),
				},
			},
		},
		{
			name: "auth failure with exit code 3",
			err: fail.DetailedError{
				Summary:  "authentication failed",
				ExitCode: intPtr(fail.ExitAuthFailure),
			},
			exitCode: fail.ExitAuthFailure,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":  "authentication failed",
					"exitCode": float64(3),
				},
			},
		},
		{
			name: "error with details",
			err: fail.DetailedError{
				Summary: "resource not found",
				Details: "no dashboard with that name exists",
			},
			exitCode: 1,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":  "resource not found",
					"exitCode": float64(1),
					"details":  "no dashboard with that name exists",
				},
			},
		},
		{
			name: "error with suggestions and docsLink",
			err: fail.DetailedError{
				Summary:     "invalid configuration",
				Suggestions: []string{"check your kubeconfig", "verify the server URL"},
				DocsLink:    "https://example.com/docs",
			},
			exitCode: 2,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":     "invalid configuration",
					"exitCode":    float64(2),
					"suggestions": []any{"check your kubeconfig", "verify the server URL"},
					"docsLink":    "https://example.com/docs",
				},
			},
		},
		{
			name: "full error with all fields",
			err: fail.DetailedError{
				Summary:     "push failed",
				Details:     "could not reach the server",
				Suggestions: []string{"check network", "verify credentials"},
				DocsLink:    "https://example.com/docs/push",
				ExitCode:    intPtr(fail.ExitPartialFailure),
			},
			exitCode: fail.ExitPartialFailure,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":     "push failed",
					"exitCode":    float64(4),
					"details":     "could not reach the server",
					"suggestions": []any{"check network", "verify credentials"},
					"docsLink":    "https://example.com/docs/push",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tt.err.WriteJSON(&buf, tt.exitCode)
			if err != nil {
				t.Fatalf("WriteJSON() returned unexpected error: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("WriteJSON() produced invalid JSON: %v\nOutput: %s", err, buf.String())
			}

			assertJSONEqual(t, tt.wantJSON, got)
		})
	}
}

func TestDetailedError_WriteJSON_NoExtraFields(t *testing.T) {
	// Verify that empty optional fields are omitted from JSON output
	err := fail.DetailedError{
		Summary: "minimal error",
	}

	var buf bytes.Buffer
	if writeErr := err.WriteJSON(&buf, 1); writeErr != nil {
		t.Fatalf("WriteJSON() returned unexpected error: %v", writeErr)
	}

	var got map[string]any
	if jsonErr := json.Unmarshal(buf.Bytes(), &got); jsonErr != nil {
		t.Fatalf("invalid JSON: %v", jsonErr)
	}

	errorObj, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' key in JSON output")
	}

	unexpectedFields := []string{"details", "suggestions", "docsLink"}
	for _, field := range unexpectedFields {
		if _, exists := errorObj[field]; exists {
			t.Errorf("expected field %q to be omitted when empty, but it was present", field)
		}
	}
}

// assertJSONEqual compares two decoded JSON maps recursively.
func assertJSONEqual(t *testing.T, want, got map[string]any) {
	t.Helper()

	for key, wantVal := range want {
		gotVal, exists := got[key]
		if !exists {
			t.Errorf("missing key %q in JSON output", key)
			continue
		}

		switch wv := wantVal.(type) {
		case map[string]any:
			gv, ok := gotVal.(map[string]any)
			if !ok {
				t.Errorf("key %q: expected object, got %T", key, gotVal)
				continue
			}
			assertJSONEqual(t, wv, gv)
		case []any:
			gv, ok := gotVal.([]any)
			if !ok {
				t.Errorf("key %q: expected array, got %T", key, gotVal)
				continue
			}
			if len(wv) != len(gv) {
				t.Errorf("key %q: expected %d items, got %d", key, len(wv), len(gv))
				continue
			}
			for i, witem := range wv {
				if witem != gv[i] {
					t.Errorf("key %q[%d]: expected %v, got %v", key, i, witem, gv[i])
				}
			}
		default:
			if wantVal != gotVal {
				t.Errorf("key %q: expected %v (%T), got %v (%T)", key, wantVal, wantVal, gotVal, gotVal)
			}
		}
	}
}
