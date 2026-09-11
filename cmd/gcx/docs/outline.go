package docs

import (
	"errors"
	"fmt"
	goio "io"
	"strconv"
	"strings"

	internaldocs "github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type outlineOpts struct {
	IO      cmdio.Options
	url     string
	product string
}

func (o *outlineOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &outlineTextCodec{})
	o.IO.BindFlags(flags)
	flags.StringVar(&o.product, "product", "", "Scope shorthand resolution to a product (used when the argument is not a full URL)")
}

func (o *outlineOpts) Validate() error {
	if strings.TrimSpace(o.url) == "" {
		return errors.New("url is required")
	}
	return o.IO.Validate()
}

// outlineHeading is the JSON-serializable form of a heading.
type outlineHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// childPage is a page nested under the outlined directory page.
type childPage struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// outlineResult wraps the heading list with the source URL and any child pages.
type outlineResult struct {
	URL        string           `json:"url"`
	Headings   []outlineHeading `json:"headings"`
	ChildPages []childPage      `json:"child_pages,omitempty"`
}

func toOutlineHeadings(headings []grafanadocs.Heading) []outlineHeading {
	out := make([]outlineHeading, len(headings))
	for i, h := range headings {
		out[i] = outlineHeading{Level: h.Level, Text: h.Text, Line: h.Line}
	}
	return out
}

func outlineCommand(loader *indexLoader, fetch docFetcher) *cobra.Command {
	opts := &outlineOpts{}
	cmd := &cobra.Command{
		Use:   "outline <url-or-query>",
		Short: "Show the heading outline of a documentation page.",
		Long: "List the headings of a documentation page so you can target a " +
			"section with 'gcx docs get --section'. The argument can be a full URL " +
			"or a shorthand query resolved via the docs index. For directory pages, " +
			"child pages from the index are included in the output.",
		Example: `  # Outline by full URL
  gcx docs outline https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/

  # Outline by shorthand query
  gcx docs outline traceql`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.url = args[0]
			if err := opts.Validate(); err != nil {
				return err
			}
			if !strings.HasPrefix(opts.url, "https://") {
				idx, err := loader.get(cmd.Context())
				if err != nil {
					return err
				}
				resolved, err := internaldocs.ResolveShorthand(idx, opts.url, opts.product)
				if err != nil {
					return err
				}
				opts.url = resolved
			}
			doc, err := fetch(cmd.Context(), opts.url)
			if err != nil {
				return cleanFetchErr(opts.url, err)
			}
			result := outlineResult{
				URL:      doc.URL,
				Headings: toOutlineHeadings(grafanadocs.Outline(doc)),
			}
			if idx, err := loader.get(cmd.Context()); err == nil {
				result.ChildPages = findChildPages(idx, doc.URL)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// findChildPages returns index entries whose URL is nested under the given
// page URL. All descendants are included, not just direct children.
func findChildPages(idx *grafanadocs.Index, pageURL string) []childPage {
	prefix := strings.TrimSuffix(pageURL, ".md")
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var children []childPage
	for _, e := range idx.Entries {
		if e.URL == pageURL {
			continue
		}
		trimmed := strings.TrimSuffix(e.URL, ".md")
		if strings.HasPrefix(trimmed, prefix) {
			children = append(children, childPage{Title: e.Title, URL: e.URL})
		}
	}
	return children
}

// outlineTextCodec renders headings as a styled LVL/HEADING/LINE table,
// followed by a CHILD PAGES table when child pages are present.
type outlineTextCodec struct{}

func (c *outlineTextCodec) Format() format.Format { return "text" }

func (c *outlineTextCodec) Encode(w goio.Writer, v any) error {
	res, ok := v.(outlineResult)
	if !ok {
		return fmt.Errorf("outlineTextCodec: expected outlineResult, got %T", v)
	}
	t := style.NewTable("LVL", "HEADING", "LINE")
	for _, h := range res.Headings {
		t.Row(strconv.Itoa(h.Level), h.Text, strconv.Itoa(h.Line))
	}
	if err := t.Render(w); err != nil {
		return err
	}
	if len(res.ChildPages) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		ct := style.NewTable("TITLE", "URL")
		for _, c := range res.ChildPages {
			ct.Row(c.Title, c.URL)
		}
		return ct.Render(w)
	}
	return nil
}

func (c *outlineTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("outline text codec does not support decoding")
}
