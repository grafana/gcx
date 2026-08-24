package docs

import (
	"errors"
	"fmt"
	goio "io"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type linksOpts struct {
	IO cmdio.Options
}

func (o *linksOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &linksTextCodec{})
	o.IO.BindFlags(flags)
}

func (o *linksOpts) Validate() error {
	return o.IO.Validate()
}

// linkEntry is the JSON-serializable form of a curated documentation link.
type linkEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// linksResult wraps the link list under a "links" key.
type linksResult struct {
	Links []linkEntry `json:"links"`
}

func toLinkEntries(links []docs.NamedLink) []linkEntry {
	out := make([]linkEntry, len(links))
	for i, l := range links {
		out[i] = linkEntry{Name: l.Name, URL: l.URL}
	}
	return out
}

func linksCommand() *cobra.Command {
	opts := &linksOpts{}
	cmd := &cobra.Command{
		Use:   "list-links",
		Short: "List curated Grafana documentation links.",
		Long: "List the curated set of canonical Grafana documentation URLs that " +
			"gcx surfaces in help text and error messages. Pass any URL to " +
			"'gcx docs get' to read the page content.",
		Example: `  gcx docs list-links`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), linksResult{
				Links: toLinkEntries(docs.AllNamed()),
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// linksTextCodec renders links as a styled NAME/URL table.
type linksTextCodec struct{}

func (c *linksTextCodec) Format() format.Format { return "text" }

func (c *linksTextCodec) Encode(w goio.Writer, v any) error {
	res, ok := v.(linksResult)
	if !ok {
		return fmt.Errorf("linksTextCodec: expected linksResult, got %T", v)
	}
	t := style.NewTable("NAME", "URL")
	for _, l := range res.Links {
		t.Row(l.Name, l.URL)
	}
	return t.Render(w)
}

func (c *linksTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("links text codec does not support decoding")
}
