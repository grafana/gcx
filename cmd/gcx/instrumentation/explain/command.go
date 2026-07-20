// Package explain implements the "gcx instrumentation explain" command tree, a
// thin wrapper around otel-checker's embedded explanation-document registry.
//
//	gcx instrumentation explain <id>   — show one doc as markdown
//	gcx instrumentation explain list   — list every registered ID and title
package explain

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/charmbracelet/glamour"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
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

// EntryView is a single row of the "explain list" output.
type EntryView struct {
	ID       string `json:"id" yaml:"id"`
	Title    string `json:"title" yaml:"title"`
	Severity string `json:"severity" yaml:"severity"`
}

// EntryListEnvelope is the canonical items envelope for JSON output
// (docs/design/output.md §101).
type EntryListEnvelope struct {
	Items []EntryView `json:"items"`
}

// Command returns the "gcx instrumentation explain" cobra command tree.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain [id]",
		Short: "Show an explanation for an otel-checker finding",
		Long: `Show a markdown explanation document for an otel-checker finding.

Each finding emitted by ` + "`gcx instrumentation check`" + ` may carry an
explain ID (e.g. env.otel-service-name.unset). Passing that ID here prints the
full explanation — what the finding means, why it matters, and how to fix it.

Powered by github.com/grafana/otel-checker.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeExplainIDs,
	}

	opts := &showOpts{}
	opts.setup(cmd.Flags())

	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("instrumentation explain: an explain ID is required (see `gcx instrumentation explain list`)")
		}
		if err := opts.IO.Validate(); err != nil {
			return fmt.Errorf("instrumentation explain: %w", err)
		}
		if err := runShow(c.OutOrStdout(), args[0], opts); err != nil {
			return fmt.Errorf("instrumentation explain: %w", err)
		}
		return nil
	}

	cmd.AddCommand(listCommand())

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
		return fmt.Errorf("unknown explain ID %q. Run `gcx instrumentation explain list` to see every available ID", id)
	}
	return opts.IO.Encode(w, DocView{
		ID:       doc.ID,
		Title:    doc.Title,
		Severity: doc.Severity,
		Body:     doc.Body,
	})
}

// listCommand returns the "gcx instrumentation explain list" subcommand.
func listCommand() *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every available explain ID with its title",
		Long: `List every registered otel-checker explain ID with its title and severity.

Use one of the IDs with ` + "`gcx instrumentation explain <id>`" + ` to see the
full explanation.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return fmt.Errorf("instrumentation explain list: %w", err)
			}
			entries := allEntries()
			envelope := EntryListEnvelope{Items: entries}
			if err := opts.IO.Encode(c.OutOrStdout(), envelope); err != nil {
				return fmt.Errorf("instrumentation explain list: %w", err)
			}
			return nil
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// listOpts is the flag/option surface for the list subcommand.
type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("table")
	o.IO.RegisterCustomCodec("table", &entryTableCodec{})
	o.IO.RegisterCustomCodec("wide", &entryTableCodec{Wide: true})
	o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(EntryView{}))
	o.IO.BindFlags(flags)
}

// allEntries builds a sorted slice of every registered explain doc.
// A stable, alphabetical order keeps output diff-friendly.
func allEntries() []EntryView {
	ids := otelexplain.All()
	entries := make([]EntryView, 0, len(ids))
	for _, id := range ids {
		doc, ok := otelexplain.Lookup(id)
		if !ok {
			continue
		}
		entries = append(entries, EntryView{
			ID:       doc.ID,
			Title:    doc.Title,
			Severity: doc.Severity,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
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

// ─── entryTableCodec ─────────────────────────────────────────────────────────

// entryTableCodec renders []EntryView as a table.
//
// Default columns: ID SEVERITY TITLE
// Wide adds no extra columns today; the flag is reserved for future use.
type entryTableCodec struct {
	Wide bool
}

var _ format.Codec = (*entryTableCodec)(nil)

func (c *entryTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *entryTableCodec) Encode(w io.Writer, v any) error {
	var entries []EntryView
	switch val := v.(type) {
	case EntryListEnvelope:
		entries = val.Items
	case []EntryView:
		entries = val
	default:
		return fmt.Errorf("entryTableCodec: expected EntryListEnvelope or []EntryView, got %T", v)
	}

	t := style.NewTable("ID", "SEVERITY", "TITLE")
	for _, e := range entries {
		t.Row(e.ID, e.Severity, e.Title)
	}
	return t.Render(w)
}

func (c *entryTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
