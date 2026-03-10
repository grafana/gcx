package fail

import (
	"encoding/json"
	"fmt"
	"io"
)

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
func (e DetailedError) WriteJSON(w io.Writer, exitCode int) error {
	envelope := errorEnvelope{
		Error: errorJSON{
			Summary:     e.Summary,
			ExitCode:    exitCode,
			Details:     e.Details,
			Suggestions: e.Suggestions,
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
// to w. Used for partial failures (FR-012) where some results succeeded and
// others failed — a single JSON object carries both the partial results and
// the error context.
func (e DetailedError) WriteJSONWithItems(w io.Writer, exitCode int, items any) error {
	type combined struct {
		Items any       `json:"items"`
		Error errorJSON `json:"error"`
	}

	env := combined{
		Items: items,
		Error: errorJSON{
			Summary:     e.Summary,
			ExitCode:    exitCode,
			Details:     e.Details,
			Suggestions: e.Suggestions,
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
