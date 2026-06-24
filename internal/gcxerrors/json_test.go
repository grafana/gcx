package gcxerrors_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func intPtr(i int) *int {
	p := new(int)
	*p = i
	return p
}

func TestDetailedError_WriteJSON(t *testing.T) {
	tests := []struct {
		name     string
		err      gcxerrors.DetailedError
		exitCode int
		wantJSON map[string]any
	}{
		{
			name: "minimal error with summary and exitCode only",
			err: gcxerrors.DetailedError{
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
			err: gcxerrors.DetailedError{
				Summary:  "authentication failed",
				ExitCode: intPtr(gcxerrors.ExitAuthFailure),
			},
			exitCode: gcxerrors.ExitAuthFailure,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":  "authentication failed",
					"exitCode": float64(3),
				},
			},
		},
		{
			name: "error with details",
			err: gcxerrors.DetailedError{
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
			err: gcxerrors.DetailedError{
				Summary:     "invalid configuration",
				Suggestions: []string{"check your kubeconfig", "verify the server URL"},
				DocsLink:    "https://example.com/docs",
			},
			exitCode: 2,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":     "invalid configuration",
					"exitCode":    float64(2),
					"suggestions": []any{"check your kubeconfig", "verify the server URL", gcxerrors.DocsFetchSuggestion("https://example.com/docs")},
					"docsLink":    "https://example.com/docs",
				},
			},
		},
		{
			name: "full error with all fields",
			err: gcxerrors.DetailedError{
				Summary:     "push failed",
				Details:     "could not reach the server",
				Suggestions: []string{"check network", "verify credentials"},
				DocsLink:    "https://example.com/docs/push",
				ExitCode:    intPtr(gcxerrors.ExitPartialFailure),
			},
			exitCode: gcxerrors.ExitPartialFailure,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":     "push failed",
					"exitCode":    float64(4),
					"details":     "could not reach the server",
					"suggestions": []any{"check network", "verify credentials", gcxerrors.DocsFetchSuggestion("https://example.com/docs/push")},
					"docsLink":    "https://example.com/docs/push",
				},
			},
		},
		{
			name: "parent folded into details when details is empty",
			err: gcxerrors.DetailedError{
				Summary: "Search failed",
				Parent:  errors.New("response body exceeds 50 MB limit; try narrowing your query or adding filters"),
			},
			exitCode: 1,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":  "Search failed",
					"exitCode": float64(1),
					"details":  "response body exceeds 50 MB limit; try narrowing your query or adding filters",
				},
			},
		},
		{
			name: "parent NOT folded when details is already set",
			err: gcxerrors.DetailedError{
				Summary: "Search failed",
				Details: "explicit details here",
				Parent:  errors.New("this should not appear in details"),
			},
			exitCode: 1,
			wantJSON: map[string]any{
				"error": map[string]any{
					"summary":  "Search failed",
					"exitCode": float64(1),
					"details":  "explicit details here",
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

// TestDetailedError_WriteJSON_DocsFetchSuggestion verifies that when DocsLink
// is set, an imperative docs-fetch suggestion (containing the URL inline) is
// appended to suggestions — so agents are actually prompted to follow the link
// rather than treating the bare docsLink field as an inert artifact.
func TestDetailedError_WriteJSON_DocsFetchSuggestion(t *testing.T) {
	t.Run("appended when DocsLink set and no prior suggestions", func(t *testing.T) {
		err := gcxerrors.DetailedError{
			Summary:  "Invalid PromQL query",
			DocsLink: "https://grafana.com/docs/grafana/latest/datasources/prometheus/query-editor.md",
		}

		var buf bytes.Buffer
		if writeErr := err.WriteJSON(&buf, 1); writeErr != nil {
			t.Fatalf("WriteJSON() returned unexpected error: %v", writeErr)
		}

		var got map[string]any
		if jsonErr := json.Unmarshal(buf.Bytes(), &got); jsonErr != nil {
			t.Fatalf("invalid JSON: %v", jsonErr)
		}
		errObj, ok := got["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected 'error' object, got %T", got["error"])
		}
		suggestions, ok := errObj["suggestions"].([]any)
		if !ok || len(suggestions) != 1 {
			t.Fatalf("expected exactly one suggestion, got %v", errObj["suggestions"])
		}
		want := gcxerrors.DocsFetchSuggestion(err.DocsLink)
		first, ok := suggestions[0].(string)
		if !ok {
			t.Fatalf("suggestion is not a string: %T", suggestions[0])
		}
		if first != want {
			t.Errorf("suggestion = %q, want %q", first, want)
		}
		// The URL must be self-contained in the suggestion (duplicated from docsLink).
		if !strings.Contains(first, err.DocsLink) {
			t.Errorf("suggestion must contain the URL inline: %q", first)
		}
	})

	t.Run("not appended when DocsLink empty", func(t *testing.T) {
		err := gcxerrors.DetailedError{
			Summary:     "something went wrong",
			Suggestions: []string{"try again"},
		}

		var buf bytes.Buffer
		if writeErr := err.WriteJSON(&buf, 1); writeErr != nil {
			t.Fatalf("WriteJSON() returned unexpected error: %v", writeErr)
		}

		var got map[string]any
		if jsonErr := json.Unmarshal(buf.Bytes(), &got); jsonErr != nil {
			t.Fatalf("invalid JSON: %v", jsonErr)
		}
		errObj, ok := got["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected 'error' object, got %T", got["error"])
		}
		suggestions, ok := errObj["suggestions"].([]any)
		if !ok {
			t.Fatalf("expected suggestions array, got %T", errObj["suggestions"])
		}
		if len(suggestions) != 1 || suggestions[0] != "try again" {
			t.Errorf("expected only the original suggestion, got %v", suggestions)
		}
	})
}

func TestDetailedError_WriteJSON_NoExtraFields(t *testing.T) {
	// Verify that empty optional fields are omitted from JSON output
	err := gcxerrors.DetailedError{
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

// TestWriteJSON_StripBoxCharsDefensive ensures that box-drawing characters
// already present in Details or Suggestions are stripped by WriteJSON.
// This is the defensive layer — even if box chars arrive from some future path,
// they must not appear in the JSON output.
func TestWriteJSON_StripBoxCharsDefensive(t *testing.T) {
	err := gcxerrors.DetailedError{
		Summary:     "push failed",
		Details:     "│ some detail with box chars ├─ end",
		Suggestions: []string{"│ Try this suggestion └─"},
	}

	var buf strings.Builder
	_ = err.WriteJSON(&buf, 1)
	output := buf.String()

	for _, ch := range []string{"│", "├", "└"} {
		if strings.Contains(output, ch) {
			t.Errorf("WriteJSON output contains box character %q after stripping:\n%s", ch, output)
		}
	}
	// Content should still be present (just with replacements).
	if !strings.Contains(output, "push failed") {
		t.Errorf("WriteJSON output missing summary:\n%s", output)
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
