package docs

import (
	"errors"
	"fmt"
	goio "io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultGetLimit is the command default and the value used when --limit
// is 0 or negative. grafanadocs.Excerpt applies the same coercion, but get
// resolves the bound itself so help text and continuation hints name a
// concrete number.
const defaultGetLimit = 80

type getOpts struct {
	IO      cmdio.Options
	url     string
	section string
	offset  int
	limit   int
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &getTextCodec{})
	o.IO.BindFlags(flags)
	flags.StringVar(&o.section, "section", "", "Heading text to extract (returns only that section)")
	flags.IntVar(&o.offset, "offset", 0, "Line offset for paging (0-indexed)")
	flags.IntVar(&o.limit, "limit", 0, "Maximum lines to return (0 or negative uses the default of 80)")
}

func (o *getOpts) Validate() error {
	if strings.TrimSpace(o.url) == "" {
		return errors.New("url is required")
	}
	if o.offset < 0 {
		return fmt.Errorf("--offset must be non-negative, got %d", o.offset)
	}
	return o.IO.Validate()
}

func (o *getOpts) validateExplicitFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("section") && strings.TrimSpace(o.section) == "" {
		return errors.New("--section must not be empty")
	}
	return nil
}

// getResult is the JSON-serializable form of a fetched, excerpted page.
type getResult struct {
	Content       string `json:"content"`
	URL           string `json:"url"`
	TotalLines    int    `json:"total_lines"`
	ReturnedRange [2]int `json:"returned_range"`
}

func getCommand(fetch docFetcher) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <url>",
		Short: "Fetch a Grafana documentation page.",
		Long: "Fetch a documentation page as cleaned markdown. Supports section " +
			"extraction and offset/limit paging for bounded retrieval.",
		Example: `  # Fetch a doc
  gcx docs get https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/

  # Extract a single section
  gcx docs get https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/ --section "Comparison operators"

  # Advanced: page through a long doc (Tempo Configure is ~3000 lines)
  gcx docs get https://grafana.com/docs/tempo/latest/configuration/ --offset 80 --limit 80`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.url = args[0]
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.validateExplicitFlags(cmd); err != nil {
				return err
			}
			doc, err := fetch(cmd.Context(), opts.url)
			if err != nil {
				return cleanFetchErr(opts.url, err)
			}
			effectiveLimit := opts.limit
			if effectiveLimit <= 0 {
				effectiveLimit = defaultGetLimit
			}
			res := grafanadocs.Excerpt(doc, grafanadocs.ExcerptOpts{
				Section: opts.section,
				Offset:  opts.offset,
				Limit:   effectiveLimit,
			})
			if res.Content == "" && opts.section != "" {
				return fmt.Errorf("section %q not found; run `gcx docs outline %s` to see available headings", opts.section, shellQuote(opts.url))
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), getResult{
				Content:       res.Content,
				URL:           doc.URL,
				TotalLines:    res.Total,
				ReturnedRange: [2]int{res.Start, res.End},
			}); err != nil {
				return err
			}
			if opts.section == "" && res.End < res.Total {
				emitGetPartialityHint(cmd.ErrOrStderr(), opts.url, res.Start, res.End, res.Total, effectiveLimit)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// getTextCodec renders the raw markdown content of a fetched page.
type getTextCodec struct{}

func (c *getTextCodec) Format() format.Format { return "text" }

func (c *getTextCodec) Encode(w goio.Writer, v any) error {
	r, ok := v.(getResult)
	if !ok {
		return fmt.Errorf("getTextCodec: expected getResult, got %T", v)
	}
	_, err := fmt.Fprintln(w, r.Content)
	return err
}

func (c *getTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("get text codec does not support decoding")
}

func emitGetPartialityHint(w goio.Writer, rawURL string, start, end, total, effectiveLimit int) {
	summary := fmt.Sprintf("showing lines %d-%d of %d", start, end, total)
	continuation := fmt.Sprintf("gcx docs get %s --offset %d --limit %d", shellQuote(rawURL), end, effectiveLimit)
	cmdio.EmitHint(w, summary, continuation)
}

func shellQuote(value string) string {
	return strconv.Quote(value)
}
