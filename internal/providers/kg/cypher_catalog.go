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
	// cypherLabelsCall matches a labels(...) call.
	cypherLabelsCall = regexp.MustCompile(`(?i)\blabels\s*\(`)
)

// catalogRowsWarning returns a warning when rows of resp hold the graph's own
// catalog records (see isGraphRecordType), or "" when none do. A catalog label
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
				if isGraphRecordType(label) && !named[label] {
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
	rowNoun, verb := "rows", "hold"
	if len(resp.Rows) == 1 {
		rowNoun = "row"
	}
	if rows == 1 {
		verb = "holds"
	}
	return fmt.Sprintf("%d of %d %s %s the graph's own catalog records (%s %s), not observed entities; "+
		"a node pattern without a label matches them too. A label on each node, or WHERE %s on an unlabeled "+
		"node n, leaves them out.",
		rows, len(resp.Rows), rowNoun, verb, noun, strings.Join(labels, ", "), strings.Join(filters, " AND "))
}

// cypherNamedLabels returns the catalog labels that query, without comments,
// names in a label position (after a colon, including each label of a label
// expression such as :A|B) or inside a string literal, where a filter such as
// labels(n)[0] <> 'Env' names them. Aliases, variables and property keys that
// equal a catalog label do not name it.
func cypherNamedLabels(query string) map[string]bool {
	named := map[string]bool{}
	addWord := func(word string) {
		if word = strings.Trim(word, "`"); isGraphRecordType(word) {
			named[word] = true
		}
	}
	// Mask string literals, so a colon inside one is not read as a label,
	// and collect the words inside them.
	rest := maskCypherStrings(query, func(literal string) {
		for _, word := range cypherWord.FindAllString(literal, -1) {
			addWord(word)
		}
	})
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

// cypherLabelColumns reports, per column, whether it projects a labels(...)
// call, at any depth, such as labels(a), labels(a)[0], head(labels(a)) or
// coalesce(labels(a)[0], 'none'). A column without an alias is named after its
// expression, so it projects one when its name contains such a call; a column
// named after an alias projects one when the expression the query gives that
// alias in a RETURN, WITH or UNWIND item contains such a call. Marking a column
// that returns something else, such as size(labels(a)), is harmless: only a
// string or a list of strings equal to a catalog label counts (see
// catalogLabelValues).
func cypherLabelColumns(query string, columns []string) []bool {
	aliases := map[string]bool{}
	for _, item := range cypherProjections(maskCypherStrings(query, nil)) {
		if item.alias != "" && cypherLabelsCall.MatchString(item.expr) {
			aliases[item.alias] = true
		}
	}
	out := make([]bool, len(columns))
	for i, column := range columns {
		out[i] = aliases[column] || cypherLabelsCall.MatchString(column)
	}
	return out
}

// cypherProjection is one item of a RETURN, WITH or UNWIND clause.
type cypherProjection struct {
	expr  string // the projected expression
	alias string // the name AS gives it, "" when it has none
}

// cypherProjections returns the items of the RETURN, WITH and UNWIND clauses
// of masked, a query without comments whose string literals are masked (see
// maskCypherStrings). An item ends at a comma at its clause's nesting depth;
// the item list ends at the next clause at that depth (see
// isCypherProjectionEnd) or where the depth drops below it. WITH after STARTS
// or ENDS is an operator, not a clause.
func cypherProjections(masked string) []cypherProjection {
	s := &projectionScanner{masked: masked, asAt: -1}
	for i := 0; i < len(masked); i++ {
		i = s.step(i)
	}
	s.closeItem(len(masked))
	return s.items
}

// projectionScanner is the state of cypherProjections.
type projectionScanner struct {
	masked      string
	items       []cypherProjection
	in          bool   // inside the item list of a clause
	base, depth int    // nesting depth of that clause, and the current one
	start       int    // offset where the current item starts
	asAt        int    // offset of the current item's AS, -1 before one
	alias       string // the current item's alias
	prev        string // the previous word, upper-cased
}

// step reads masked from offset i and returns the offset of the last byte it
// read.
func (s *projectionScanner) step(i int) int {
	c := s.masked[i]
	switch {
	case c == '`':
		return s.quotedName(i)
	case c == '(' || c == '[' || c == '{':
		s.depth++
	case c == ')' || c == ']' || c == '}':
		s.depth--
		if s.in && s.depth < s.base {
			s.closeItem(i)
			s.in = false
		}
	case c == ',' && s.atItemLevel():
		s.closeItem(i)
		s.start = i + 1
	case isCypherWordStart(c) && (i == 0 || !isCypherWordByte(s.masked[i-1]) && s.masked[i-1] != '.'):
		return s.word(i)
	}
	return i
}

// quotedName reads the backtick-quoted name that starts at offset i, the
// current item's alias when it follows AS.
func (s *projectionScanner) quotedName(i int) int {
	end := strings.IndexByte(s.masked[i+1:], '`')
	if end < 0 {
		end = len(s.masked) - i - 1
	}
	if s.atItemLevel() && s.prev == "AS" && s.asAt >= 0 && s.alias == "" {
		s.alias = s.masked[i+1 : i+1+end]
	}
	s.prev = ""
	return i + end + 1
}

// word reads the word that starts at offset i. A word after a dot, such as a
// property key, is not read as one.
func (s *projectionScanner) word(i int) int {
	j := i
	for j < len(s.masked) && isCypherWordByte(s.masked[j]) {
		j++
	}
	word := strings.ToUpper(s.masked[i:j])
	switch {
	case word == "WITH" && (s.prev == "STARTS" || s.prev == "ENDS"):
		// The STARTS WITH and ENDS WITH operators.
	case (word == "RETURN" || word == "WITH" || word == "UNWIND") && (!s.in || s.depth == s.base):
		s.closeItem(i)
		s.in, s.base, s.start = true, s.depth, j
	case !s.atItemLevel():
	case isCypherProjectionEnd(word):
		s.closeItem(i)
		s.in = false
	case word == "AS":
		s.asAt = i
	case s.prev == "AS" && s.asAt >= 0 && s.alias == "":
		s.alias = s.masked[i:j]
	case word == "DISTINCT" && strings.TrimSpace(s.masked[s.start:i]) == "":
		s.start = j
	}
	s.prev = word
	return j - 1
}

func (s *projectionScanner) atItemLevel() bool { return s.in && s.depth == s.base }

// closeItem ends the current item at offset at.
func (s *projectionScanner) closeItem(at int) {
	if !s.in {
		return
	}
	end := at
	if s.asAt >= 0 {
		end = s.asAt
	}
	if expr := strings.TrimSpace(s.masked[s.start:end]); expr != "" {
		s.items = append(s.items, cypherProjection{expr: expr, alias: s.alias})
	}
	s.asAt, s.alias = -1, ""
}

// isCypherProjectionEnd reports whether word, upper-cased, starts a clause
// other than RETURN, WITH and UNWIND that ends the item list of one of them.
func isCypherProjectionEnd(word string) bool {
	switch word {
	case "CALL", "LIMIT", "MATCH", "OPTIONAL", "ORDER", "SKIP", "UNION", "WHERE":
		return true
	default:
		return false
	}
}

func isCypherWordStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isCypherWordByte(c byte) bool {
	return isCypherWordStart(c) || c >= '0' && c <= '9'
}

// maskCypherStrings returns query with the text inside its '...' and "..."
// string literals replaced by spaces, so offsets stay the same, and calls
// literal, unless it is nil, with the text inside each. Backtick-quoted names
// are kept, quote characters inside them included.
func maskCypherStrings(query string, literal func(string)) string {
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
			if literal != nil {
				literal(query[i+1 : j])
			}
			for k := i + 1; k < j; k++ {
				masked[k] = ' '
			}
		}
		i = j
	}
	return string(masked)
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
