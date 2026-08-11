package docs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
)

// fetchDoc is the seam through which get/outline reach the network. It points
// at grafanadocs.FetchDoc in production and is replaced in tests so the
// success paths can be exercised without a live fetch (mirroring the
// CommandWithIndex hook used for the index-backed commands).
var fetchDoc = grafanadocs.FetchDoc

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
// The index is fetched on the first subcommand that needs it (search,
// products) and cached for the lifetime of the process. Commands that only
// need FetchDoc (get, outline) never trigger the load.
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
	return newDocsCommand(&indexLoader{})
}

// CommandWithIndex returns a docs command group wired to a pre-loaded index.
// Intended for tests — avoids network fetches during test execution.
func CommandWithIndex(idx *grafanadocs.Index) *cobra.Command {
	loader := &indexLoader{idx: idx}
	loader.once.Do(func() {})
	return newDocsCommand(loader)
}

func newDocsCommand(loader *indexLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Search and read Grafana documentation.",
		Long: "Search, fetch, and outline Grafana Labs product documentation, " +
			"backed by an in-memory index of grafana.com docs.",
	}

	cmd.AddCommand(
		searchCommand(loader),
		getCommand(),
		outlineCommand(),
		productsCommand(loader),
		linksCommand(),
	)

	return cmd
}
