package check

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/cmd/gcx/instrumentation/check/fixplan"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// ResultsWithFixPlan is the JSON/YAML envelope emitted by `gcx instrumentation
// check`. When --fix-plan is not set, FixPlan is nil and the JSON serialization
// matches the pre-fixplan shape (`otelutils.Results` fields as top-level keys).
//
// Fields are declared explicitly rather than via an embedded otelutils.Results
// so that reflect-based JSON field discovery (cmdio.MakeFieldValidator, which
// powers `--json <field>`) sees them: reflect.Type.Fields does not yield
// promoted fields from anonymous embeds.
type ResultsWithFixPlan struct {
	Checks   []otelutils.ComponentResult `json:"checks" yaml:"checks"`
	Warnings []otelutils.ComponentResult `json:"warnings" yaml:"warnings"`
	Errors   []otelutils.ComponentResult `json:"errors" yaml:"errors"`

	FixPlan *fixplan.Plan `json:"fix_plan,omitempty" yaml:"fix_plan,omitempty"`
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
	envelope, ok := v.(ResultsWithFixPlan)
	if !ok {
		return errCheckTableCodecExpectedResults
	}

	t := style.NewTable("STATUS", "COMPONENT", "MESSAGE", "EXPLAIN_ID")
	for _, r := range envelope.Errors {
		t.Row("FAIL", r.Component, r.Message, r.ExplainID)
	}
	for _, r := range envelope.Warnings {
		t.Row("WARN", r.Component, r.Message, r.ExplainID)
	}
	for _, r := range envelope.Checks {
		t.Row("OK", r.Component, r.Message, r.ExplainID)
	}
	if err := t.Render(w); err != nil {
		return err
	}

	if envelope.FixPlan == nil || envelope.FixPlan.Content == "" {
		return nil
	}
	return renderFixPlan(w, envelope.FixPlan)
}

func (c *CheckTableCodec) Decode(_ io.Reader, _ any) error {
	return errCheckTableCodecNoDecode
}

// renderFixPlan prints a blank separator line then the plan body via
// style.RenderMarkdown (glamour-styled on a TTY, raw markdown otherwise).
// No source diagnostics are emitted here: --fix-plan=assistant either
// succeeds (Assistant content speaks for itself) or errors out at the
// command level, and --fix-plan=local produces its own headered markdown.
// This keeps `gcx instrumentation check --fix-plan=local > out.md` clean.
func renderFixPlan(w io.Writer, plan *fixplan.Plan) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return style.RenderMarkdown(w, plan.Content)
}

var (
	errCheckTableCodecExpectedResults = errors.New("CheckTableCodec: expected ResultsWithFixPlan")
	errCheckTableCodecNoDecode        = errors.New("table format does not support decoding")
)
