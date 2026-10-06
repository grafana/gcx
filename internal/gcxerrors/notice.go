package gcxerrors

import (
	"encoding/json"
	"fmt"
	"io"
)

// ErrorNoticeClass tags the advisory stderr copy of an in-band error. It
// extends the typed stderr classes (hint, warning, note).
const ErrorNoticeClass = "error"

// noticeFieldLimit bounds each notice field; the complete error document
// stays on stdout.
const noticeFieldLimit = 500

type errorNotice struct {
	Class      string `json:"class"`
	Summary    string `json:"summary"`
	ExitCode   int    `json:"exitCode"`
	Suggestion string `json:"suggestion,omitempty"`
}

// WriteNotice writes a one-line JSONL copy of an error to w, normally stderr.
//
// Shell pipelines such as `gcx ... | jq '.data'` consume the stdout error
// document, and the pipeline exit status belongs to the last command, so the
// caller sees only the filter's output (for example `null`). The notice keeps
// the failure visible. It is advisory: stdout still carries the authoritative
// error document.
//
// The notice never carries error details. They can quote a raw HTTP response
// body, and gcx has no redaction for error text. Stderr often ends up in logs
// even when a filter drops the stdout document.
func WriteNotice(w io.Writer, summary string, suggestions []string, exitCode int) error {
	notice := errorNotice{
		Class:    ErrorNoticeClass,
		Summary:  clip(stripBoxChars(summary)),
		ExitCode: exitCode,
	}
	if len(suggestions) > 0 {
		notice.Suggestion = clip(stripBoxChars(suggestions[0]))
	}

	data, err := json.Marshal(notice)
	if err != nil {
		return fmt.Errorf("marshaling error notice: %w", err)
	}

	_, err = fmt.Fprintln(w, string(data))
	return err
}

// WriteNotice writes the advisory stderr copy of e. See [WriteNotice].
func (e DetailedError) WriteNotice(w io.Writer, exitCode int) error {
	return WriteNotice(w, e.Summary, e.agentSuggestions(), exitCode)
}

func clip(s string) string {
	runes := []rune(s)
	if len(runes) <= noticeFieldLimit {
		return s
	}
	// The ellipsis counts toward the limit.
	return string(runes[:noticeFieldLimit-1]) + "…"
}
