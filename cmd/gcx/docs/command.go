package docs

import (
	"context"
	"fmt"
	goio "io"
	"os"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/docsindex"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
)

// docFetcher fetches a documentation page as bounded Markdown. It is
// grafanadocs.FetchDoc in production and is replaced in tests so the
// get/outline success paths can be exercised without a live fetch.
// Dependency injection keeps it off the package scope.
type docFetcher func(ctx context.Context, url string) (*grafanadocs.Doc, error)

// cleanFetchErr rewrites a grafanadocs fetch error into product-facing
// language: it adds the URL for context and strips the internal
// "grafanadocs:" package prefix so users and agents see a clean message
// (for example, `rejected host "evil.com" (only grafana.com allowed)`) rather
// than a leaked implementation detail. The library exposes no sentinel errors
// to match on, so the descriptive message text is preserved as-is.
func cleanFetchErr(rawURL string, err error) error {
	msg := strings.ReplaceAll(err.Error(), "grafanadocs: ", "")
	return fmt.Errorf("fetching %s: %s", rawURL, msg)
}

// indexLoader provides lazy, once-only loading of the documentation index.
// The index is fetched on the first subcommand that needs it and cached for
// the lifetime of the process. Commands that always use the index: search,
// list-products. Commands that use it conditionally: get and outline load the
// index only when the argument is a shorthand query (not a full URL).
//
// Lazy loading avoids a network fetch on unrelated commands or --help.
type indexLoader struct {
	once sync.Once
	idx  *grafanadocs.Index
	err  error
}

func (l *indexLoader) get(ctx context.Context) (*grafanadocs.Index, error) {
	l.once.Do(func() {
		// DOCS_INDEX_URL is an unadvertised override for pointing at a mirror
		// or a fixture during testing; it is intentionally not a user-facing
		// flag. Any value must still be an https URL (enforced by LoadIndex).
		url := grafanadocs.DefaultIndexURL
		if override := os.Getenv("DOCS_INDEX_URL"); override != "" {
			url = override
		}
		l.idx, l.err = grafanadocs.LoadIndex(ctx, url)
		if l.err != nil {
			l.err = fmt.Errorf("loading docs index: %w", l.err)
		}
	})
	return l.idx, l.err
}

// Command returns the "docs" command group. Mount it on the root command
// with rootCmd.AddCommand(docs.Command()).
func Command() *cobra.Command {
	return newDocsCommand(&indexLoader{}, grafanadocs.FetchDoc)
}

// resolveIfShorthand resolves a URL-or-query argument to a full documentation
// URL. If the input contains a URL scheme ("://"), it is returned as-is so
// FetchDoc can validate or reject the scheme. Otherwise the docs index is
// loaded and the shorthand query is resolved via search, optionally scoped to
// a product.
func resolveIfShorthand(ctx context.Context, loader *indexLoader, input, product string) (string, error) {
	if strings.Contains(input, "://") {
		return input, nil
	}
	idx, err := loader.get(ctx)
	if err != nil {
		return "", err
	}
	return docsindex.ResolveShorthand(idx, input, product)
}

func emitShorthandResolutionHint(w goio.Writer, subcommand, original, resolved string) {
	if original == resolved {
		return
	}
	cmdio.EmitHint(w,
		fmt.Sprintf("resolved %q to documentation URL", original),
		fmt.Sprintf("gcx docs %s %q", subcommand, resolved),
	)
}

func newDocsCommand(loader *indexLoader, fetch docFetcher) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Search and read Grafana documentation.",
		Long: "Search, fetch, and outline Grafana Labs product documentation, " +
			"backed by an in-memory index of grafana.com docs.",
	}

	cmd.AddCommand(
		searchCommand(loader),
		getCommand(loader, fetch),
		outlineCommand(loader, fetch),
		productsCommand(loader),
		linksCommand(),
	)

	return cmd
}
