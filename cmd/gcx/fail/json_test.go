package fail_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
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
	err := &fail.DetailedError{
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
