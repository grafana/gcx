package fail_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	gcxerrors "github.com/grafana/gcx/internal/gcxerrors"
)

func intPtr(i int) *int {
	p := new(int)
	*p = i
	return p
}

// TestWriteJSON_NoBoxChars ensures that when a *DetailedError is passed through
// ErrorToDetailedError and then WriteJSON, no box-drawing characters appear in
// the JSON output. Prior to the errors.As fix, *DetailedError fell through to
// fallbackDetailedError which called err.Error(), producing box chars in Details.
func TestWriteJSON_NoBoxChars(t *testing.T) {
	err := &gcxerrors.DetailedError{
		Summary:     "cluster not found",
		Details:     `cluster "x" has no config`,
		Suggestions: []string{"Run: gcx instrumentation clusters list"},
	}
	converted := fail.ErrorToDetailedError(err)

	var buf strings.Builder
	_ = converted.WriteJSON(&buf, 1)
	output := buf.String()

	for _, ch := range []string{"│", "├", "─", "└", "┌", "┐", "┘"} {
		if strings.Contains(output, ch) {
			t.Errorf("WriteJSON output contains box character %q:\n%s", ch, output)
		}
	}
	// Verify the actual content is preserved, not lost.
	if !strings.Contains(output, "cluster not found") {
		t.Errorf("WriteJSON output missing summary:\n%s", output)
	}
}

// TestWriteJSON_FallbackErrorIncludesDetails verifies that when an unrecognised
// wrapped error falls through to fallbackDetailedError, the inner error message
// appears in the JSON "details" field via Parent folding.
func TestWriteJSON_FallbackErrorIncludesDetails(t *testing.T) {
	inner := errors.New("response body exceeds 50 MB limit; try narrowing your query or adding filters")
	err := fmt.Errorf("search failed: %w", inner)

	converted := fail.ErrorToDetailedError(err)

	var buf strings.Builder
	_ = converted.WriteJSON(&buf, 1)

	var got map[string]any
	if jsonErr := json.Unmarshal([]byte(buf.String()), &got); jsonErr != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", jsonErr, buf.String())
	}

	errObj, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' key in JSON output")
	}

	details, _ := errObj["details"].(string)
	if details == "" {
		t.Errorf("expected non-empty 'details' in JSON, got: %s", buf.String())
	}
	if !strings.Contains(details, "50 MB limit") {
		t.Errorf("expected details to contain inner error message, got: %q", details)
	}
}
