package check

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/glamour"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	otelutils "github.com/grafana/otel-checker/checks/utils"
	"golang.org/x/term"
)

// ResultsWithFixPlan is the JSON/YAML envelope emitted by `gcx instrumentation
// check`. When --fix-plan is not set, FixPlan is nil and the JSON serialization
// matches the pre-fixplan shape (`otelutils.Results` fields as top-level keys)
// thanks to the embedded struct.
type ResultsWithFixPlan struct {
	otelutils.Results

	FixPlan *FixPlanEnvelope `json:"fix_plan,omitempty" yaml:"fix_plan,omitempty"`
}

// FixPlanEnvelope carries the fix-plan output.
//
// Source is "assistant" when Grafana Assistant produced the plan and "local"
// when the local aggregator did. Fallback is true only in the second case
// (Assistant was requested but unreachable); Reason explains why.
type FixPlanEnvelope struct {
	Source   string   `json:"source" yaml:"source"`
	Content  string   `json:"content" yaml:"content"`
	DocsUsed []string `json:"docs_used,omitempty" yaml:"docs_used,omitempty"`
	Fallback bool     `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	Reason   string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// CheckTableCodec renders otelutils.Results as a grouped status/component/
// message table.
//
// Default columns: STATUS COMPONENT MESSAGE EXPLAIN_ID
// Wide adds no extra columns today; the flag is reserved for future use
// (e.g. raw severity codes) and accepted to satisfy the gcx output
// convention of having distinct table/wide codecs.
//
// EXPLAIN_ID is left empty for findings that do not carry one (typically
// successful checks). Pass a non-empty ID to `gcx instrumentation explain
// <id>` to see the full explanation.
//
// When a fix plan is attached, the codec prints the table first, then a
// separator and the plan's markdown body (styled via glamour on a TTY, raw
// otherwise) so both surfaces are visible in one command.
type CheckTableCodec struct {
	Wide bool
}

var _ format.Codec = (*CheckTableCodec)(nil)

func (c *CheckTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *CheckTableCodec) Encode(w io.Writer, v any) error {
	var results otelutils.Results
	var plan *FixPlanEnvelope
	switch val := v.(type) {
	case ResultsWithFixPlan:
		results = val.Results
		plan = val.FixPlan
	case otelutils.Results:
		results = val
	default:
		return errCheckTableCodecExpectedResults
	}

	t := style.NewTable("STATUS", "COMPONENT", "MESSAGE", "EXPLAIN_ID")
	for _, r := range results.Errors {
		t.Row("FAIL", r.Component, r.Message, r.ExplainID)
	}
	for _, r := range results.Warnings {
		t.Row("WARN", r.Component, r.Message, r.ExplainID)
	}
	for _, r := range results.Checks {
		t.Row("OK", r.Component, r.Message, r.ExplainID)
	}
	if err := t.Render(w); err != nil {
		return err
	}

	if plan == nil || plan.Content == "" {
		return nil
	}
	return renderFixPlan(w, plan)
}

func (c *CheckTableCodec) Decode(_ io.Reader, _ any) error {
	return errCheckTableCodecNoDecode
}

// renderFixPlan prints a separator, a source notice, and the plan body.
// When the destination is a terminal, the plan (which is markdown) is styled
// via glamour; otherwise it's written raw so piping/tests stay clean.
func renderFixPlan(w io.Writer, plan *FixPlanEnvelope) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	switch plan.Source {
	case "local":
		if plan.Fallback && plan.Reason != "" {
			fmt.Fprintf(w, "Grafana Assistant not available (%s). Showing combined explanation docs instead — no AI reasoning applied.\n\n", plan.Reason)
		} else {
			fmt.Fprintln(w, "Showing combined explanation docs — no AI reasoning applied.")
			fmt.Fprintln(w)
		}
	case "assistant":
		// No leading notice; Assistant output speaks for itself.
	}

	if isTerminalWriter(w) {
		if out, err := glamour.Render(plan.Content, "dark"); err == nil {
			_, err := fmt.Fprint(w, out)
			return err
		}
		// Fall through to raw on glamour failure.
	}
	_, err := fmt.Fprint(w, plan.Content)
	return err
}

// isTerminalWriter reports whether w is an *os.File attached to a terminal.
// Buffers, pipes, and non-file writers are treated as non-terminal so raw
// markdown flows through cleanly.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

var (
	errCheckTableCodecExpectedResults = errors.New("CheckTableCodec: expected otelutils.Results or ResultsWithFixPlan")
	errCheckTableCodecNoDecode        = errors.New("table format does not support decoding")
)
