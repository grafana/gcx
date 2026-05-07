package fail

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// boxCharsReplacer replaces Unicode box-drawing characters with plain ASCII
// equivalents. This is a defensive measure: the primary fix is the errors.As
// correction in ErrorToDetailedError, but any box chars that arrive in Details
// or Suggestions (e.g., from future code paths) are stripped here so they
// never leak into agent-mode JSON output.
var boxCharsReplacer = strings.NewReplacer( //nolint:gochecknoglobals
	"│", "|", "├", "+", "─", "-", "└", "+",
	"┌", "+", "┐", "+", "┘", "+", "▶", ">",
	"◆", "*", "●", "*",
)

func stripBoxChars(s string) string {
	return boxCharsReplacer.Replace(s)
}

// errorJSON is the JSON representation of a DetailedError.
// Optional fields use pointers so they are omitted when empty.
type errorJSON struct {
	Summary     string   `json:"summary"`
	ExitCode    int      `json:"exitCode"`
	Details     string   `json:"details,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
	DocsLink    string   `json:"docsLink,omitempty"`
}

// errorEnvelope is the top-level JSON object written to stdout on error.
type errorEnvelope struct {
	Error errorJSON `json:"error"`
}

// WriteJSON writes the error as a JSON object to the given writer.
// The output shape is: {"error": {"summary": "...", "exitCode": N, ...}}
// Optional fields (details, suggestions, docsLink) are omitted when empty.
// The exitCode in JSON matches the process exit code derived from ExitCode.
// Box-drawing characters in Details and Suggestions are replaced with plain
// ASCII equivalents as a defensive measure against rendering artefacts in
// agent-mode JSON output.
func (e DetailedError) WriteJSON(w io.Writer, exitCode int) error {
	sug := make([]string, len(e.Suggestions))
	for i, s := range e.Suggestions {
		sug[i] = stripBoxChars(s)
	}
	envelope := errorEnvelope{
		Error: errorJSON{
			Summary:     e.Summary,
			ExitCode:    exitCode,
			Details:     stripBoxChars(e.Details),
			Suggestions: sug,
			DocsLink:    e.DocsLink,
		},
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshaling error JSON: %w", err)
	}

	_, err = fmt.Fprintln(w, string(data))
	return err
}

// WriteJSONWithItems writes a combined {"items": [...], "error": {...}} envelope
// to w. Used for partial failures where some results succeeded and
// others failed — a single JSON object carries both the partial results and
// the error context.
func (e DetailedError) WriteJSONWithItems(w io.Writer, exitCode int, items any) error {
	type combined struct {
		Items any       `json:"items"`
		Error errorJSON `json:"error"`
	}

	sug := make([]string, len(e.Suggestions))
	for i, s := range e.Suggestions {
		sug[i] = stripBoxChars(s)
	}
	env := combined{
		Items: items,
		Error: errorJSON{
			Summary:     e.Summary,
			ExitCode:    exitCode,
			Details:     stripBoxChars(e.Details),
			Suggestions: sug,
			DocsLink:    e.DocsLink,
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshaling partial failure envelope: %w", err)
	}

	_, err = fmt.Fprintln(w, string(data))
	return err
}
