package docs

import (
	"errors"
	"fmt"
	goio "io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultSearchLimit is the command default and the value used when --limit
// is 0 or negative. grafanadocs.Search applies the same coercion, but search
// resolves the bound itself so it can over-fetch by one and attach honest
// list_meta (the library returns no total and treats <=0 as 5, so
// BindListLimit's "0 means all" would be a lie).
const defaultSearchLimit = 5

// emptySearchHint returns a hint message when search yields no results.
func emptySearchHint(product string) string {
	if product != "" {
		return "no results found; try broadening the product filter or run 'gcx docs list-products' to see available products"
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

// searchResult is the single shape passed to every codec. JSON/YAML
// serialize the envelope; the text codec extracts .Results to render rows.
// list_meta is attached only when the page is truncated (over-fetch proved
// more hits exist).
type searchResult struct {
	Results  []searchEntry   `json:"results"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
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
  gcx docs search "traceql query" --product tempo

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
			// Resolve the bound before the library sees it so we can request
			// one extra hit. A spare row is the only honest "has more"
			// signal — Search returns no total.
			effectiveLimit := opts.limit
			if effectiveLimit <= 0 {
				effectiveLimit = defaultSearchLimit
			}
			hits := grafanadocs.Search(idx, opts.query, grafanadocs.SearchOpts{
				Product: opts.product,
				Limit:   effectiveLimit + 1,
			})
			entries, meta := cmdio.TruncatePagedList(toSearchEntries(hits), effectiveLimit)
			meta = cmdio.AttachListMeta(meta, os.Args)
			if len(entries) == 0 {
				cmdio.EmitHint(cmd.ErrOrStderr(), emptySearchHint(opts.product), "")
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), &searchResult{
				Results:  entries,
				ListMeta: meta,
			}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// searchTextCodec renders search hits as a styled TITLE/PRODUCT/URL table.
type searchTextCodec struct{}

func (c *searchTextCodec) Format() format.Format { return "text" }

func (c *searchTextCodec) Encode(w goio.Writer, v any) error {
	res, ok := v.(*searchResult)
	if !ok {
		return fmt.Errorf("searchTextCodec: expected *searchResult, got %T", v)
	}
	t := style.NewTable("TITLE", "PRODUCT", "URL")
	for _, e := range res.Results {
		t.Row(e.Title, e.Product, e.URL)
	}
	return t.Render(w)
}

func (c *searchTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("search text codec does not support decoding")
}
