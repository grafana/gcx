package kg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// cypherNamedLabel matches a label in a label position (after a colon, or a
// further label of a label expression such as :A|B) or a quoted string: the
// two ways a query names a catalog label it asks for or filters out.
var cypherNamedLabel = regexp.MustCompile("[:|&]\\s*`?([A-Za-z_][A-Za-z0-9_]*)|'([^']*)'|\"([^\"]*)\"")

// catalogRowsWarning returns a warning when rows of resp hold the graph's own
// catalog records (see isCatalogRecordType), or "" when none do.
//
// A row holds one when a returned node, also inside a path, list or map,
// carries a catalog label, or, in a query that calls labels(...), when a
// value outside node properties is a catalog label string. Catalog labels the
// query names itself (see cypherNamedLabel) are not counted: the query asks
// for them or filters them out. This is a hint built from the response and a
// rough scan of the query, not a Cypher parser: a string such as
// n.name <> 'Schema' also counts as naming a label, and rows that project only
// properties of catalog records are not detected. Rows are never changed.
func catalogRowsWarning(query string, resp *CypherQueryResponse) string {
	if resp == nil || len(resp.Rows) == 0 {
		return ""
	}
	named := map[string]bool{}
	for _, m := range cypherNamedLabel.FindAllStringSubmatch(query, -1) {
		named[m[1]+m[2]+m[3]] = true
	}
	strs := strings.Contains(strings.ToLower(query), "labels(")
	found := map[string]bool{}
	rows := 0
	for _, row := range resp.Rows {
		inRow := false
		add := func(label string) {
			if isCatalogRecordType(label) && !named[label] {
				found[label] = true
				inRow = true
			}
		}
		for _, cell := range row {
			var value any
			if err := json.Unmarshal(cell, &value); err == nil {
				catalogLabels(value, strs, add)
			}
		}
		if inRow {
			rows++
		}
	}
	if rows == 0 {
		return ""
	}
	labels := make([]string, 0, len(found))
	for label := range found {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	filters := make([]string, len(labels))
	for i, label := range labels {
		filters[i] = fmt.Sprintf("type <> '%s'", label)
	}
	noun := "label"
	if len(labels) > 1 {
		noun = "labels"
	}
	rowNoun, verb := "rows", "hold"
	if len(resp.Rows) == 1 {
		rowNoun = "row"
	}
	if rows == 1 {
		verb = "holds"
	}
	// A WHERE on labels(n) right after MATCH can exceed the endpoint's time
	// limit on a large graph where the same comparisons on a WITH alias do
	// not, so the filter compares an alias.
	return fmt.Sprintf("%d of %d %s %s the graph's own catalog records (%s %s), not observed entities; "+
		"a node pattern without a label matches them too. A label on each node leaves them out. For an "+
		"unlabeled node n, filter its label after a WITH, as in WITH n, labels(n)[0] AS type WHERE %s, and "+
		"list any other variable used later in that WITH; a WHERE on labels(n) right after MATCH can time out "+
		"on a large graph.",
		rows, len(resp.Rows), rowNoun, verb, noun, strings.Join(labels, ", "), strings.Join(filters, " AND "))
}

// catalogLabels calls add for the labels of every node in value, including
// nodes nested in paths, lists and maps, and, when strs is set, for every
// other string in value outside node properties.
func catalogLabels(value any, strs bool, add func(string)) {
	switch v := value.(type) {
	case string:
		if strs {
			add(v)
		}
	case map[string]any:
		node := v["kind"] == "node"
		if labels, ok := v["labels"].([]any); ok && node {
			for _, label := range labels {
				if s, ok := label.(string); ok {
					add(s)
				}
			}
		}
		for key, item := range v {
			if key == "properties" || (node && key == "labels") {
				continue
			}
			catalogLabels(item, strs, add)
		}
	case []any:
		for _, item := range v {
			catalogLabels(item, strs, add)
		}
	}
}
