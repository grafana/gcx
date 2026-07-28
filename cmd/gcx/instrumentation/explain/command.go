// Package explain implements two sibling cobra commands under
// `gcx instrumentation` that surface otel-checker's embedded explanation-
// document registry:
//
//	gcx instrumentation explain <id>       — show one doc as markdown
//	gcx instrumentation list-explanations  — enumerate every registered ID
//
// The two commands share the doc registry access and codec types but are
// mounted independently in `cmd/gcx/instrumentation/command.go`.
package explain

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/glamour"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	otelexplain "github.com/grafana/otel-checker/checks/explain"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// DocView is the JSON/YAML-friendly projection of an explain doc.
// Mirrors otelexplain.Doc but keeps the field-tag surface stable if the
// upstream struct grows internal fields.
type DocView struct {
	ID       string `json:"id" yaml:"id"`
	Title    string `json:"title" yaml:"title"`
	Severity string `json:"severity" yaml:"severity"`
	Body     string `json:"body" yaml:"body"`
}

// Command returns the "gcx instrumentation explain <id>" cobra command. It
// takes exactly one positional (the explain ID) and prints the doc. Use
// [ListCommand] for the enumeration surface.
func Command() *cobra.Command {
	opts := &showOpts{}

	cmd := &cobra.Command{
		Use:   "explain <id>",
		Short: "Show an explanation for an otel-checker finding",
		Long: `Show a markdown explanation document for an otel-checker finding.

Each finding emitted by ` + "`gcx instrumentation check`" + ` may carry an
explain ID (e.g. env.otel-service-name.unset). Passing that ID here prints the
full explanation — what the finding means, why it matters, and how to fix it.

To see every available ID, run ` + "`gcx instrumentation list-explanations`" + `.

Powered by github.com/grafana/otel-checker.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeExplainIDs,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return fmt.Errorf("instrumentation explain: %w", err)
			}
			if err := runShow(c.OutOrStdout(), args[0], opts); err != nil {
				return fmt.Errorf("instrumentation explain: %w", err)
			}
			return nil
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// showOpts is the flag/option surface for the single-ID show path.
type showOpts struct {
	IO cmdio.Options
}

func (o *showOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &docTextCodec{})
	o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(DocView{}))
	o.IO.BindFlags(flags)
}

// runShow looks up the doc and encodes it through the configured codec.
// For text output the codec falls through to markdown rendering; JSON/YAML
// serialize the DocView struct directly.
func runShow(w io.Writer, id string, opts *showOpts) error {
	doc, ok := otelexplain.Lookup(id)
	if !ok {
		return fmt.Errorf("unknown explain ID %q. Run `gcx instrumentation list-explanations` to see every available ID", id)
	}
	return opts.IO.Encode(w, DocView{
		ID:       doc.ID,
		Title:    doc.Title,
		Severity: doc.Severity,
		Body:     doc.Body,
	})
}

// completeExplainIDs supplies dynamic completion for the ID positional arg.
func completeExplainIDs(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return otelexplain.All(), cobra.ShellCompDirectiveNoFileComp
}

// ─── docTextCodec ────────────────────────────────────────────────────────────

// docTextCodec renders a DocView as markdown. When the destination is a
// terminal, glamour styles the markdown; otherwise the raw markdown is written
// so that piping (`gcx instrumentation explain <id> | grep`) and tests stay
// clean.
type docTextCodec struct{}

var _ format.Codec = (*docTextCodec)(nil)

func (*docTextCodec) Format() format.Format { return "text" }

func (*docTextCodec) Encode(w io.Writer, v any) error {
	doc, ok := v.(DocView)
	if !ok {
		return fmt.Errorf("docTextCodec: expected DocView, got %T", v)
	}
	source := fmt.Sprintf("# %s\n\n%s", doc.Title, doc.Body)
	if isTerminalWriter(w) {
		if out, err := glamour.Render(source, "dark"); err == nil {
			_, err := fmt.Fprint(w, out)
			return err
		}
		// Fall through to raw markdown if glamour fails.
	}
	_, err := fmt.Fprint(w, source)
	return err
}

func (*docTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
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
