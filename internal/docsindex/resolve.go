package docsindex

import (
	"fmt"
	"strings"

	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
)

// ResolveShorthand resolves a short input to a full grafana.com docs URL via
// index search. The input must not contain a URL scheme; callers treat any
// argument containing "://" as a literal URL before invoking this function.
//
// The returned URL comes from the index (which only contains
// https://grafana.com/ entries per I11) and is re-validated by FetchDoc's
// allowlist (I3/I21) before any network call.
func ResolveShorthand(idx *grafanadocs.Index, input string, product string) (string, error) {
	hits := grafanadocs.Search(idx, input, grafanadocs.SearchOpts{
		Product: product,
		Limit:   1,
	})
	if len(hits) == 0 {
		cmd := "gcx docs search"
		if product != "" {
			cmd += " --product " + shellQuote(product)
		}
		cmd += " " + shellQuote(input)
		msg := fmt.Sprintf("no matching page found; try `%s` to browse results", cmd)
		if product != "" {
			msg += "; run `gcx docs list-products` to see available products"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return hits[0].URL, nil
}

// shellQuote wraps val in single quotes, escaping any embedded single quotes
// using the canonical POSIX form (end-quote, backslash-escaped quote, re-open-quote).
// Used to safely embed user-controlled values in shell command suggestions.
func shellQuote(val string) string {
	return "'" + strings.ReplaceAll(val, "'", `'\''`) + "'"
}
