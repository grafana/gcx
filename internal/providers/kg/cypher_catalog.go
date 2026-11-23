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
	// cypherName matches a whole plain or backtick-quoted name.
	cypherName = regexp.MustCompile("^(?:`([^`]*)`|([A-Za-z_][A-Za-z0-9_]*))$")
)

// catalogRowsWarning returns a warning when rows of resp hold the graph's own
// catalog records (see isCatalogRecordType), or "" when none do. A catalog
// label the query names itself (see cypherNamedLabels) is wanted or filtered
// out by the query, so rows that carry only such labels are not counted; rows
// with other catalog labels still are. A row holds one when a returned node
// (also inside a path, list or map) carries a catalog label, or when a column
// that projects labels(...) returns one (see cypherLabelColumns).
//
// Rows that project only properties of such a record are not detected, and
// neither are returned relationships: the response gives a relationship's
// ends as refs, not labels, so a relationship between catalog records is
// detected only through a row that also returns one of its ends as a node.
func catalogRowsWarning(query string, resp *CypherQueryResponse) string {
	if resp == nil || len(resp.Rows) == 0 {
		return ""
	}
	query = stripCypherComments(query)
	aliases, labelAliases := cypherLabelAliases(maskCypherStrings(query, nil))
	named := cypherNamedLabels(query, labelAliases)
	shapes := cypherLabelColumns(resp.Columns, aliases)
	found := map[string]bool{}
	rows := 0
	for _, row := range resp.Rows {
		inRow := false
		for i, cell := range row {
			var shape *labelShape
			if i < len(shapes) {
				shape = shapes[i]
			}
			if shape == nil && !bytes.Contains(cell, []byte(`"labels"`)) {
				continue
			}
			var value any
			if err := json.Unmarshal(cell, &value); err != nil {
				continue
			}
			add := func(label string) {
				if isCatalogRecordType(label) && !named[label] {
					found[label] = true
					inRow = true
				}
			}
			catalogLabelValues(value, shape, add)
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
		filters[i] = fmt.Sprintf("type <> '%s'", label)
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

// cypherNamedLabels returns the catalog labels that query, without comments,
// names itself: in a label position (after a colon, as in a pattern label or
// a label predicate such as WHERE n:Label, including each label of a label
// expression such as :A|B), or in a string literal compared with a labels(...)
// expression or with an alias in labelAliases, as in labels(n)[0] <> 'X',
// 'X' IN labels(n) or type <> 'X' after WITH labels(n)[0] AS type. A string
// counts when the predicate around it (see cypherPredicate) holds such an
// expression, so a string in a property filter such as n.name <> 'X' does not
// name a label. Aliases, variables and property keys that equal a catalog
// label do not name it either.
func cypherNamedLabels(query string, labelAliases map[string]bool) map[string]bool {
	named := map[string]bool{}
	addWord := func(word string) {
		if word = strings.Trim(word, "`"); isCatalogRecordType(word) {
			named[word] = true
		}
	}
	// Mask string literals, so a colon inside one is not read as a label,
	// and note where they are.
	var literals [][2]int
	rest := maskCypherStrings(query, func(start, end int) {
		literals = append(literals, [2]int{start, end})
	})
	isLabelAlias := func(name string) bool { return labelAliases[name] }
	for _, literal := range literals {
		// The operand is the literal with its quotes.
		predicate := cypherPredicate(rest, literal[0]-1, min(literal[1]+1, len(rest)))
		if cypherLabelsCall.MatchString(predicate) || cypherRefersTo(predicate, isLabelAlias) {
			for _, word := range cypherWord.FindAllString(query[literal[0]:literal[1]], -1) {
				addWord(word)
			}
		}
	}
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

// cypherPredicate returns the text of the predicate that holds the operand
// masked[start:end], where masked is a query without comments whose string
// literals are masked (see maskCypherStrings). An operand inside a list
// literal stands for that list, as in x IN ['a', 'b']. The predicate reaches
// out from the operand, over bracketed groups, to the nearest comma or
// unclosed bracket at its depth, or to a word that ends a predicate (see
// isCypherPredicateBoundary), such as AND, WHERE or THEN.
func cypherPredicate(masked string, start, end int) string {
	for {
		open := cypherOpenBefore(masked, start)
		if open < 0 || masked[open] != '[' {
			break
		}
		closing := cypherCloseAfter(masked, open+1)
		if closing < 0 {
			break
		}
		start, end = open, closing+1
	}
	return masked[cypherPredicateStart(masked, start):cypherPredicateEnd(masked, end)]
}

// cypherPredicateStart returns the offset where the predicate that holds the
// operand starting at offset start begins (see cypherPredicate).
func cypherPredicateStart(masked string, start int) int {
	for i := start - 1; i >= 0; i-- {
		c := masked[i]
		switch {
		case c == '`':
			i = strings.LastIndexByte(masked[:i], '`')
			if i < 0 {
				return 0
			}
		case c == ')' || c == ']' || c == '}':
			i = cypherOpenBefore(masked, i)
			if i < 0 {
				return 0
			}
		case c == '(' || c == '[' || c == '{' || c == ',':
			return i + 1
		case isCypherWordByte(c):
			j := i
			for j > 0 && isCypherWordByte(masked[j-1]) {
				j--
			}
			word := strings.ToUpper(masked[j : i+1])
			if word == "WITH" && isCypherStringOperatorStart(cypherWordBefore(masked, j)) {
				word = ""
			}
			if isCypherPredicateBoundary(word) && (j == 0 || masked[j-1] != '.') {
				return i + 1
			}
			i = j
		}
	}
	return 0
}

// cypherPredicateEnd returns the offset where the predicate that holds the
// operand ending at offset end ends (see cypherPredicate).
func cypherPredicateEnd(masked string, end int) int {
	prev := ""
	for i := end; i < len(masked); i++ {
		c := masked[i]
		switch {
		case c == '`':
			j := strings.IndexByte(masked[i+1:], '`')
			if j < 0 {
				return len(masked)
			}
			i += j + 1
		case c == '(' || c == '[' || c == '{':
			i = cypherCloseAfter(masked, i+1)
			if i < 0 {
				return len(masked)
			}
		case c == ')' || c == ']' || c == '}' || c == ',':
			return i
		case isCypherWordStart(c) && (i == 0 || !isCypherWordByte(masked[i-1])):
			j := i
			for j < len(masked) && isCypherWordByte(masked[j]) {
				j++
			}
			word := strings.ToUpper(masked[i:j])
			isKey := i > 0 && masked[i-1] == '.'
			if word == "WITH" && isCypherStringOperatorStart(prev) {
				isKey = true
			}
			if isCypherPredicateBoundary(word) && !isKey {
				return i
			}
			prev = word
			i = j - 1
		}
	}
	return len(masked)
}

// cypherWordBefore returns the word, upper-cased, that ends before offset at
// with only spaces between, or "" when there is none.
func cypherWordBefore(masked string, at int) string {
	end := len(strings.TrimRight(masked[:at], " \t\r\n"))
	start := end
	for start > 0 && isCypherWordByte(masked[start-1]) {
		start--
	}
	return strings.ToUpper(masked[start:end])
}

// isCypherStringOperatorStart reports whether word, upper-cased, makes a
// following WITH the STARTS WITH or ENDS WITH operator, not a clause.
func isCypherStringOperatorStart(word string) bool {
	return word == "STARTS" || word == "ENDS"
}

// isCypherPredicateBoundary reports whether word, upper-cased, ends a
// predicate: a boolean operator, a CASE keyword or a clause keyword.
func isCypherPredicateBoundary(word string) bool {
	switch word {
	case "AND", "OR", "XOR", "NOT", "CASE", "WHEN", "THEN", "ELSE", "END",
		"WHERE", "RETURN", "WITH", "UNWIND", "AS", "DISTINCT", "MATCH", "OPTIONAL",
		"ORDER", "BY", "SKIP", "LIMIT", "CALL", "YIELD", "UNION":
		return true
	default:
		return false
	}
}

// cypherOpenBefore returns the offset of the innermost bracket of masked
// that is still open at offset at, skipping bracketed groups and
// backtick-quoted names, or -1 when there is none. For a closing bracket at
// offset at, that is the bracket it closes.
func cypherOpenBefore(masked string, at int) int {
	depth := 0
	for i := at - 1; i >= 0; i-- {
		switch masked[i] {
		case '`':
			if i = strings.LastIndexByte(masked[:i], '`'); i < 0 {
				return -1
			}
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// cypherCloseAfter returns the offset of the first bracket of masked from
// offset at on that closes a bracket opened before at, skipping bracketed
// groups and backtick-quoted names, or -1 when there is none. For an opening
// bracket at offset at-1, that is the bracket that closes it.
func cypherCloseAfter(masked string, at int) int {
	depth := 0
	for i := at; i < len(masked); i++ {
		switch masked[i] {
		case '`':
			j := strings.IndexByte(masked[i+1:], '`')
			if j < 0 {
				return -1
			}
			i += j + 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// labelShape says where a value holds labels(...) output.
type labelShape struct {
	// whole means the value itself is labels(...) output: a label or a list
	// of them.
	whole bool
	// keys holds, for a map, the shape of each key whose value holds such
	// output.
	keys map[string]*labelShape
}

// cypherLabelAliases reads the RETURN, WITH and UNWIND items of masked, a
// query without comments whose string literals are masked (see
// cypherProjections), in query order. It returns the shape (see
// cypherLabelShape) of each alias as the query leaves it, nil for an alias
// that holds no labels(...) output, and every alias that holds such output at
// some point of the query. An item that gives an alias a new expression
// replaces the earlier one, as in WITH labels(a)[0] AS t, a WITH a.name AS t.
func cypherLabelAliases(masked string) (map[string]*labelShape, map[string]bool) {
	aliases := map[string]*labelShape{}
	labelAliases := map[string]bool{}
	for _, item := range cypherProjections(masked) {
		if item.alias == "" {
			continue
		}
		shape := cypherLabelShape(item.expr, aliases)
		aliases[item.alias] = shape
		if shape != nil {
			labelAliases[item.alias] = true
		}
	}
	return aliases, labelAliases
}

// cypherLabelColumns returns, per column, where it holds labels(...) output
// (see cypherLabelShape), or nil for a column that holds none. A column named
// after an alias has that alias's shape (see cypherLabelAliases); a column
// without an alias is named after its expression, so it has the shape of
// that expression. Marking a column that returns something else, such as
// size(labels(a)), is harmless: only a string or a list of strings equal to a
// catalog label counts (see catalogLabelValues).
func cypherLabelColumns(columns []string, aliases map[string]*labelShape) []*labelShape {
	out := make([]*labelShape, len(columns))
	for i, column := range columns {
		if shape := aliases[column]; shape != nil {
			out[i] = shape
			continue
		}
		out[i] = cypherLabelShape(maskCypherStrings(column, nil), aliases)
	}
	return out
}

// cypherLabelShape returns where expr, whose string literals are masked (see
// maskCypherStrings), holds labels(...) output, or nil when it holds none. A
// bare alias holds it as its expression did (see cypherLabelAliases). A map
// literal or map projection, such as {t: labels(a)[0], name: a.name} or
// a {.name, t: labels(a)[0]}, holds it in the keys whose values hold it. Any
// other expression holds it whole when it contains a labels(...) call at any
// depth, such as labels(a)[0], head(labels(a)) or coalesce(labels(a)[0],
// 'none'), or refers to an alias that holds it (see cypherRefersTo), as t does
// after WITH labels(a)[0] AS t and ls[0] after WITH labels(a) AS ls.
func cypherLabelShape(expr string, aliases map[string]*labelShape) *labelShape {
	expr = strings.TrimSpace(expr)
	if name, ok := cypherBareName(expr); ok {
		return aliases[name]
	}
	if entries, ok := cypherMapEntries(expr); ok {
		keys := map[string]*labelShape{}
		for _, entry := range entries {
			if shape := cypherLabelShape(entry.expr, aliases); shape != nil {
				keys[entry.key] = shape
			}
		}
		if len(keys) == 0 {
			return nil
		}
		return &labelShape{keys: keys}
	}
	isLabelAlias := func(name string) bool { return aliases[name] != nil }
	if cypherLabelsCall.MatchString(expr) || cypherRefersTo(expr, isLabelAlias) {
		return &labelShape{whole: true}
	}
	return nil
}

// cypherBareName returns the name that expr is, without backticks, and true,
// when expr is a single plain or backtick-quoted name.
func cypherBareName(expr string) (string, bool) {
	m := cypherName.FindStringSubmatch(expr)
	if m == nil {
		return "", false
	}
	return m[1] + m[2], true
}

// cypherMapEntry is one key of a map literal or map projection.
type cypherMapEntry struct {
	key  string // the key, without backticks
	expr string // the key's value expression
}

// cypherMapEntries returns the keys of expr and true when expr, whose string
// literals are masked, is a map literal such as {k: v} or a map projection
// such as a {.name, k: v, x}. A property selector (.name or .*) gives no
// key; a variable selector x gives the key x with the value x.
func cypherMapEntries(expr string) ([]cypherMapEntry, bool) {
	open := strings.IndexByte(expr, '{')
	if open < 0 || cypherCloseAfter(expr, open+1) != len(expr)-1 {
		return nil, false
	}
	if prefix := strings.TrimSpace(expr[:open]); prefix != "" {
		if _, ok := cypherBareName(prefix); !ok {
			return nil, false
		}
	}
	var entries []cypherMapEntry
	body := expr[open+1 : len(expr)-1]
	for _, item := range cypherSplitItems(body) {
		item = strings.TrimSpace(item)
		if item == "" || item[0] == '.' {
			continue
		}
		if colon := cypherTopLevelIndex(item, ':'); colon >= 0 {
			if key, ok := cypherBareName(strings.TrimSpace(item[:colon])); ok {
				entries = append(entries, cypherMapEntry{key: key, expr: item[colon+1:]})
			}
			continue
		}
		if key, ok := cypherBareName(item); ok {
			entries = append(entries, cypherMapEntry{key: key, expr: item})
		}
	}
	return entries, true
}

// cypherSplitItems splits masked at its commas outside brackets and
// backtick-quoted names.
func cypherSplitItems(masked string) []string {
	var items []string
	start := 0
	for {
		comma := cypherTopLevelIndex(masked[start:], ',')
		if comma < 0 {
			return append(items, masked[start:])
		}
		items = append(items, masked[start:start+comma])
		start += comma + 1
	}
}

// cypherTopLevelIndex returns the offset of the first c in masked outside
// brackets and backtick-quoted names, or -1 when there is none.
func cypherTopLevelIndex(masked string, c byte) int {
	depth := 0
	for i := 0; i < len(masked); i++ {
		switch masked[i] {
		case '`':
			j := strings.IndexByte(masked[i+1:], '`')
			if j < 0 {
				return -1
			}
			i += j + 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case c:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// cypherRefersTo reports whether expr, whose string literals are masked (see
// maskCypherStrings), refers to an alias for which isAlias reports true: names
// it as a word, plain or backtick-quoted, that does not follow a dot, as a
// property key does.
func cypherRefersTo(expr string, isAlias func(string) bool) bool {
	for i := 0; i < len(expr); i++ {
		start := i
		var name string
		switch c := expr[i]; {
		case c == '`':
			end := strings.IndexByte(expr[i+1:], '`')
			if end < 0 {
				end = len(expr) - i - 1
			}
			name = expr[i+1 : i+1+end]
			i += end + 1
		case isCypherWordStart(c) && (i == 0 || !isCypherWordByte(expr[i-1])):
			j := i
			for j < len(expr) && isCypherWordByte(expr[j]) {
				j++
			}
			name = expr[i:j]
			i = j - 1
		default:
			continue
		}
		before := strings.TrimRight(expr[:start], " \t\r\n")
		isKey := strings.HasSuffix(before, ".") && !strings.HasSuffix(before, "..")
		if isAlias(name) && !isKey {
			return true
		}
	}
	return false
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
// literal, unless it is nil, with the offsets of the text inside each, so
// query[start:end] is that text. Backtick-quoted names are kept, quote
// characters inside them included.
func maskCypherStrings(query string, literal func(start, end int)) string {
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
				literal(i+1, j)
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

// catalogLabelValues calls add for the labels(...) output in value, a value of
// the given shape (see cypherLabelShape): a label string or a list of them,
// also under the keys of a map that hold one.
func catalogLabelValues(value any, shape *labelShape, add func(string)) {
	switch {
	case shape == nil:
	case shape.whole:
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
	default:
		if m, ok := value.(map[string]any); ok {
			for key, keyShape := range shape.keys {
				catalogLabelValues(m[key], keyShape, add)
			}
		}
	}
}

// catalogNodeLabels calls add for the labels of every node in value, including
// nodes nested in paths, lists and maps.
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
