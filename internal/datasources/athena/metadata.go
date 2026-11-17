package athena

import (
	"fmt"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/athena"
)

// filterAndLimit narrows a fully-fetched discovery listing to the names that
// contain filter (literal, case-insensitive) and then caps it at limit. A limit
// of 0 means unbounded. When the cap drops matches, it warns on warnw and names
// both escape hatches, so a capped listing never reads as the complete
// inventory.
//
// The notice goes out as a typed warn plus a hint, rather than as
// cmdio.Warning's prose line, so agent-mode stderr stays JSONL like the other
// SQL datasources' truncation notices.
func filterAndLimit(warnw io.Writer, items []string, filter string, limit int, noun string) []string {
	items = athena.FilterNames(items, filter)
	if limit <= 0 || len(items) <= limit {
		return items
	}
	cmdio.EmitWarn(warnw, fmt.Sprintf("showing the first %d %s; more %s match", limit, noun, noun))
	cmdio.EmitHint(warnw, "narrow the listing, or remove the cap", "--filter SUBSTRING / --limit 0")
	return items[:limit]
}
