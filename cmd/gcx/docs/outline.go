package docs

import (
	"errors"
	"fmt"
	goio "io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type outlineOpts struct {
	IO  cmdio.Options
	url string
}

func (o *outlineOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &outlineTextCodec{})
	o.IO.BindFlags(flags)
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

// outlineResult wraps the heading list with the source URL.
type outlineResult struct {
	URL      string           `json:"url"`
	Headings []outlineHeading `json:"headings"`
}

func toOutlineHeadings(headings []grafanadocs.Heading) []outlineHeading {
	out := make([]outlineHeading, len(headings))
	for i, h := range headings {
		out[i] = outlineHeading{Level: h.Level, Text: h.Text, Line: h.Line}
	}
	return out
}

func outlineCommand(fetch docFetcher) *cobra.Command {
	opts := &outlineOpts{}
	cmd := &cobra.Command{
		Use:   "outline <url>",
		Short: "Show the heading outline of a documentation page.",
		Long: "List the headings of a documentation page so you can target a " +
			"section with 'gcx docs get --section'.",
		Example: `  gcx docs outline https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.url = args[0]
			if err := opts.Validate(); err != nil {
				return err
			}
			doc, err := fetch(cmd.Context(), opts.url)
			if err != nil {
				return cleanFetchErr(opts.url, err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), outlineResult{
				URL:      doc.URL,
				Headings: toOutlineHeadings(grafanadocs.Outline(doc)),
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// outlineTextCodec renders headings as a styled LVL/HEADING/LINE table.
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
	return t.Render(w)
}

func (c *outlineTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("outline text codec does not support decoding")
}
