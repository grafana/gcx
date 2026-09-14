// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package docs

import (
	"fmt"
	"strings"

	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
)

// ResolveShorthand resolves a short input to a full grafana.com docs URL
// via index search. If the input already looks like a full URL (starts with
// "https://"), it is returned unchanged. Otherwise the input is treated as a
// search query against the loaded index and the top hit's URL is returned.
//
// The returned URL comes from the index (which only contains
// https://grafana.com/ entries per I11) and is re-validated by FetchDoc's
// allowlist (I3/I21) before any network call.
//
// Security: resolved URLs must only be passed to FetchDoc. They must not
// be passed to DocsFetchSuggestion or interpolated into shell commands;
// those paths require trusted constant values from the docs link registry.
func ResolveShorthand(idx *grafanadocs.Index, input string, product string) (string, error) {
	if strings.HasPrefix(input, "https://") {
		return input, nil
	}

	hits := grafanadocs.Search(idx, input, grafanadocs.SearchOpts{
		Product: product,
		Limit:   1,
	})
	if len(hits) == 0 {
		hint := "no matching page found; try 'gcx docs search"
		if product != "" {
			hint += " --product " + shellQuote(product)
		}
		hint += " " + shellQuote(input) + "' to browse results"
		return "", fmt.Errorf("%s", hint)
	}
	return hits[0].URL, nil
}

// shellQuote wraps val in single quotes, escaping any embedded single quotes
// using the canonical POSIX form (end-quote, backslash-escaped quote, re-open-quote).
// Used to safely embed user-controlled values in shell command suggestions.
func shellQuote(val string) string {
	return "'" + strings.ReplaceAll(val, "'", `'\''`) + "'"
}
