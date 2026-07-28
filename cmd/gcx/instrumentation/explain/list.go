package explain

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	otelexplain "github.com/grafana/otel-checker/checks/explain"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// EntryView is a single row of the list-explanations output.
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

// ListCommand returns the "gcx instrumentation list-explanations" cobra
// command. It's mounted as a sibling of Command() under `gcx instrumentation`.
func ListCommand() *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:     "list-explanations",
		Short:   "List every available otel-checker explain ID",
		Aliases: []string{"list-explains"},
		Long: `List every registered otel-checker explain ID with its title and severity.

Use one of the IDs with ` + "`gcx instrumentation explain <id>`" + ` to see the
full explanation for a specific finding.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return fmt.Errorf("instrumentation list-explanations: %w", err)
			}
			envelope := EntryListEnvelope{Items: allEntries()}
			if err := opts.IO.Encode(c.OutOrStdout(), envelope); err != nil {
				return fmt.Errorf("instrumentation list-explanations: %w", err)
			}
			return nil
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// listOpts is the flag/option surface for list-explanations.
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

// allEntries returns every registered explain doc as an EntryView, in the
// alphabetical order produced by otelexplain.All(). No additional sort — the
// upstream registry already yields sorted IDs.
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
	return entries
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
