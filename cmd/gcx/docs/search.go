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

// defaultSearchLimit mirrors grafanadocs.Search's internal default (applied
// when Limit <= 0). It is duplicated here only so search can compute the
// effective cap for the truncation hint; the library remains the source of
// truth for the actual limiting.
const defaultSearchLimit = 5

// emptySearchHint returns a hint message when search yields no results.
func emptySearchHint(product string) string {
	if product != "" {
		return "no results found; try broadening the product filter or run 'gcx docs products' to see available products"
	}
	return "no results found; try different search terms"
}

// truncatedSearchHint returns a completeness hint for when a result set fills
// the limit exactly. grafanadocs.Search returns no total, so more matches may
// exist beyond the cap; a bare-array result cannot carry that signal in the
// payload, so it is disclosed on stderr.
func truncatedSearchHint(limit int) string {
	return fmt.Sprintf("showing the top %d results; raise --limit or refine the query to see more", limit)
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
	flags.StringVar(&o.product, "product", "", "Filter results to a specific product (case-insensitive; matches exact, then prefix, then substring; empty = all products)")
	flags.IntVar(&o.limit, "limit", defaultSearchLimit, "Maximum number of results (0 or negative uses the default)")
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
		// ArbitraryArgs (rather than ExactArgs(1)) lets a multi-word query be
		// passed unquoted — `gcx docs search rate limiting` — by joining the
		// args into a single phrase. The empty-query case is caught in
		// Validate. Sibling commands that take a single URL use ExactArgs(1).
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
			// Mirror the library's <=0 coercion so the hint reports the
			// cap that was actually applied.
			effectiveLimit := opts.limit
			if effectiveLimit <= 0 {
				effectiveLimit = defaultSearchLimit
			}
			switch {
			case len(results) == 0:
				cmdio.EmitHint(cmd.ErrOrStderr(), emptySearchHint(opts.product), "")
			case len(results) == effectiveLimit:
				cmdio.EmitHint(cmd.ErrOrStderr(), truncatedSearchHint(effectiveLimit), "")
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
