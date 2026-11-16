package kg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	// cypherWord matches identifiers, used on the text of string literals.
	cypherWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	// cypherLabelStart matches a label after a colon, as in (n:Label),
	// [r:TYPE] or WHERE n:Label, plain or backtick-quoted.
	cypherLabelStart = regexp.MustCompile("^:\\s*(`[^`]+`|[A-Za-z_][A-Za-z0-9_]*)")
	// cypherLabelNext matches a further label of a label expression, as in
	// :A|B, :A&B or :A:B.
	cypherLabelNext = regexp.MustCompile("^\\s*[:|&]\\s*(`[^`]+`|[A-Za-z_][A-Za-z0-9_]*)")
	// cypherLabelsCall matches the start of a labels(...) call.
	cypherLabelsCall = regexp.MustCompile(`(?i)\blabels\s*\(`)
	// cypherLabelsAlias matches what may follow a labels(...) call up to its
	// alias: an optional index, an optional closing parenthesis of a wrapping
	// head(...), then AS and the alias, which it captures.
	cypherLabelsAlias = regexp.MustCompile("(?is)^\\s*(?:\\[\\s*\\d+\\s*\\])?\\s*\\)?\\s+AS\\s+(`[^`]+`|[A-Za-z_][A-Za-z0-9_]*)")
)

// catalogRowsWarning returns a warning when rows of resp hold the graph's own
// catalog records (see isMetaEntityType), or "" when none do. A catalog label
// the query names itself, as a label or inside a string literal (see
// cypherNamedLabels), is wanted or filtered out by the query, so rows that
// carry only such labels are not counted; rows with other catalog labels
// still are. A row holds one when a returned node (also inside
// a path or list) carries a catalog label, or when a column that projects
// labels(...) returns one. Rows that project only properties of such a record
// are not detected.
func catalogRowsWarning(query string, resp *CypherQueryResponse) string {
	if resp == nil || len(resp.Rows) == 0 {
		return ""
	}
	query = stripCypherComments(query)
	named := cypherNamedLabels(query)
	labelColumns := cypherLabelColumns(query, resp.Columns)
	found := map[string]bool{}
	rows := 0
	for _, row := range resp.Rows {
		inRow := false
		for i, cell := range row {
			isLabelColumn := i < len(labelColumns) && labelColumns[i]
			if !isLabelColumn && !bytes.Contains(cell, []byte(`"labels"`)) {
				continue
			}
			var value any
			if err := json.Unmarshal(cell, &value); err != nil {
				continue
			}
			add := func(label string) {
				if isMetaEntityType(label) && !named[label] {
					found[label] = true
					inRow = true
				}
			}
			if isLabelColumn {
				catalogLabelValues(value, add)
			}
			catalogNodeLabels(value, add)
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
	noun := "label"
	if len(labels) > 1 {
		noun = "labels"
	}
	filters := make([]string, len(labels))
	for i, label := range labels {
		filters[i] = fmt.Sprintf("labels(n)[0] <> '%s'", label)
	}
	return fmt.Sprintf("%d of %d rows hold the graph's own catalog records (%s %s), which describe its entity types, "+
		"the relationship types between them and its env values, not observed entities; a node pattern without "+
		"a label matches them too. A label on each node, or WHERE %s on an unlabeled node n, leaves them out.",
		rows, len(resp.Rows), noun, strings.Join(labels, ", "), strings.Join(filters, " AND "))
}

// cypherNamedLabels returns the catalog labels that query, without comments,
// names in a label position (after a colon, including each label of a label
// expression such as :A|B) or inside a string literal, where a filter such as
// labels(n)[0] <> 'Env' names them. Aliases, variables and property keys that
// equal a catalog label do not name it.
func cypherNamedLabels(query string) map[string]bool {
	named := map[string]bool{}
	addWord := func(word string) {
		if word = strings.Trim(word, "`"); isMetaEntityType(word) {
			named[word] = true
		}
	}
	// Mask string literals, so a colon inside one is not read as a label,
	// and collect the words inside them.
	masked := []byte(query)
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c != '\'' && c != '"' && c != '`' {
			continue
		}
		j := i + 1
		for j < len(query) && query[j] != c {
			if query[j] == '\\' && c != '`' {
				j++
			}
			j++
		}
		j = min(j, len(query))
		if c != '`' {
			for _, word := range cypherWord.FindAllString(query[i+1:j], -1) {
				addWord(word)
			}
			for k := i + 1; k < j; k++ {
				masked[k] = ' '
			}
		}
		i = j
	}
	rest := string(masked)
	for i := 0; i < len(rest); i++ {
		if rest[i] != ':' {
			continue
		}
		m := cypherLabelStart.FindStringSubmatchIndex(rest[i:])
		if m == nil {
			continue
		}
		addWord(rest[i+m[2] : i+m[3]])
		end := i + m[1]
		for {
			n := cypherLabelNext.FindStringSubmatchIndex(rest[end:])
			if n == nil {
				break
			}
			addWord(rest[end+n[2] : end+n[3]])
			end += n[1]
		}
		i = end - 1
	}
	return named
}

// cypherLabelColumns reports, per column, whether it projects labels(...):
// either unaliased (the column is named after the expression) or through an
// alias the query gives a labels(...) expression.
func cypherLabelColumns(query string, columns []string) []bool {
	aliases := map[string]bool{}
	for _, loc := range cypherLabelsCall.FindAllStringIndex(query, -1) {
		end := closingParen(query, loc[1]-1)
		if end < 0 {
			continue
		}
		if m := cypherLabelsAlias.FindStringSubmatch(query[end+1:]); m != nil {
			aliases[strings.Trim(m[1], "`")] = true
		}
	}
	out := make([]bool, len(columns))
	for i, column := range columns {
		name := strings.ToLower(strings.ReplaceAll(column, " ", ""))
		out[i] = aliases[column] || strings.HasPrefix(name, "labels(") || strings.HasPrefix(name, "head(labels(")
	}
	return out
}

// closingParen returns the index of the parenthesis that closes the one at
// open in s, at any nesting depth, or -1 when it is not closed.
func closingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// stripCypherComments returns query without its // and /* */ comments. String
// literals and backtick-quoted names are kept as they are, comment markers
// inside them included.
func stripCypherComments(query string) string {
	var b strings.Builder
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for j < len(query) && query[j] != c {
				if query[j] == '\\' && c != '`' {
					j++
				}
				j++
			}
			j = min(j+1, len(query))
			b.WriteString(query[i:j])
			i = j - 1
		case strings.HasPrefix(query[i:], "//"):
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				return b.String()
			}
			i += end - 1
		case strings.HasPrefix(query[i:], "/*"):
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			b.WriteByte(' ')
			i += 2 + end + 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// catalogLabelValues calls add for a label column's value: a label string or
// a list of them.
func catalogLabelValues(value any, add func(string)) {
	switch v := value.(type) {
	case string:
		add(v)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}
}

// catalogNodeLabels calls add for the labels of every node in value, including
// nodes nested in paths and lists.
func catalogNodeLabels(value any, add func(string)) {
	switch v := value.(type) {
	case map[string]any:
		if v["kind"] == "node" {
			if labels, ok := v["labels"].([]any); ok {
				for _, label := range labels {
					if s, ok := label.(string); ok {
						add(s)
					}
				}
			}
		}
		for key, item := range v {
			if key == "properties" {
				continue
			}
			catalogNodeLabels(item, add)
		}
	case []any:
		for _, item := range v {
			catalogNodeLabels(item, add)
		}
	}
}
