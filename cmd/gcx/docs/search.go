package docs

import (
	"errors"
	"fmt"
	goio "io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// emptySearchHint returns a hint message when search yields no results.
func emptySearchHint(product string) string {
	if product != "" {
		return "no results found; try broadening the product filter or run 'gcx docs products' to see available products"
	}
	return "no results found; try different search terms"
}

type searchOpts struct {
	IO      cmdio.Options
	query   string
	product string
	limit   int
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &searchTextCodec{})
	o.IO.BindFlags(flags)
	flags.StringVar(&o.product, "product", "", "Filter results to a specific product")
	flags.IntVar(&o.limit, "limit", 5, "Maximum number of results")
}

func (o *searchOpts) Validate() error {
	if strings.TrimSpace(o.query) == "" {
		return errors.New("query is required")
	}
	return o.IO.Validate()
}

// searchEntry is the JSON-serializable form of a search hit.
type searchEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Product     string `json:"product"`
}

func toSearchEntries(entries []grafanadocs.Entry) []searchEntry {
	out := make([]searchEntry, len(entries))
	for i, e := range entries {
		out[i] = searchEntry{
			Title:       e.Title,
			URL:         e.URL,
			Description: e.Description,
			Product:     e.Product,
		}
	}
	return out
}

func searchCommand(loader *indexLoader) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search Grafana documentation.",
		Long:  "Search the documentation index by keyword. Returns matching pages ranked by relevance.",
		Example: `  # Search across all products
  gcx docs search "rate limiting"

  # Scope the search to one product
  gcx docs search "metrics generator" --product tempo

  # Return more results as JSON
  gcx docs search dashboards --limit 10 -o json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.query = strings.Join(args, " ")
			if err := opts.Validate(); err != nil {
				return err
			}
			idx, err := loader.get(cmd.Context())
			if err != nil {
				return err
			}
			results := grafanadocs.Search(idx, opts.query, grafanadocs.SearchOpts{
				Product: opts.product,
				Limit:   opts.limit,
			})
			if len(results) == 0 {
				cmdio.EmitHint(cmd.ErrOrStderr(), emptySearchHint(opts.product), "")
			}
			return opts.IO.Encode(cmd.OutOrStdout(), toSearchEntries(results))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// searchTextCodec renders search hits as a styled TITLE/PRODUCT/URL table.
type searchTextCodec struct{}

func (c *searchTextCodec) Format() format.Format { return "text" }

func (c *searchTextCodec) Encode(w goio.Writer, v any) error {
	entries, ok := v.([]searchEntry)
	if !ok {
		return fmt.Errorf("searchTextCodec: expected []searchEntry, got %T", v)
	}
	t := style.NewTable("TITLE", "PRODUCT", "URL")
	for _, e := range entries {
		t.Row(e.Title, e.Product, e.URL)
	}
	return t.Render(w)
}

func (c *searchTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("search text codec does not support decoding")
}
