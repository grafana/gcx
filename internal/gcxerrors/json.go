package gcxerrors

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

// DocsFetchSuggestion returns the imperative instruction appended to agent
// JSON output when a DocsLink is set. A bare "docsLink" field does not
// reliably prompt an agent to act on it; suggestions is the field agents
// treat as actionable, so the URL is intentionally duplicated here as a
// self-contained, fetchable instruction.
func DocsFetchSuggestion(url string) string {
	return "If the cause isn't clear from the details, fetch the documentation at " + url + " for guidance before retrying."
}

// agentSuggestions returns the suggestions for agent JSON output: the
// caller's suggestions with box-drawing characters stripped, plus a
// docs-fetch nudge when DocsLink is set.
func (e DetailedError) agentSuggestions() []string {
	sug := make([]string, 0, len(e.Suggestions)+1)
	for _, s := range e.Suggestions {
		sug = append(sug, stripBoxChars(s))
	}
	if e.DocsLink != "" {
		sug = append(sug, DocsFetchSuggestion(e.DocsLink))
	}
	return sug
}

// resolvedDetails returns Details if non-empty, otherwise falls back to
// Parent.Error() so that wrapped errors surfaced via fallbackDetailedError
// still carry context in the JSON output.
func (e DetailedError) resolvedDetails() string {
	if e.Details != "" {
		return e.Details
	}
	if e.Parent != nil {
		return e.Parent.Error()
	}
	return ""
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
// agent-mode JSON output. When DocsLink is set, an imperative docs-fetch
// suggestion is appended to suggestions so agents are actually prompted to
// follow the link (see DocsFetchSuggestion).
func (e DetailedError) WriteJSON(w io.Writer, exitCode int) error {
	envelope := errorEnvelope{
		Error: errorJSON{
			Summary:     e.Summary,
			ExitCode:    exitCode,
			Details:     stripBoxChars(e.resolvedDetails()),
			Suggestions: e.agentSuggestions(),
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

	env := combined{
		Items: items,
		Error: errorJSON{
			Summary:     e.Summary,
			ExitCode:    exitCode,
			Details:     stripBoxChars(e.resolvedDetails()),
			Suggestions: e.agentSuggestions(),
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
