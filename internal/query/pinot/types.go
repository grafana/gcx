package pinot

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

const (
	// DatasourceType is the Grafana plugin ID for StarTree Pinot datasources.
	DatasourceType = "startree-pinot-datasource"

	// DefaultLimit is the typed `gcx datasources pinot query` --limit default.
	DefaultLimit = 100
	// MaxLimit is the ceiling EnforceLimit will emit. Callers warn when they cap.
	MaxLimit = 1000
)

// leadingSetRe matches one or more Pinot SET statements at the start of a
// query (e.g. SET useMultistageEngine = true;). Those prefixes are stripped
// before the SELECT-shaped allow-list so EnforceLimit can still bound the
// SELECT that follows.
var leadingSetRe = regexp.MustCompile(`(?is)^(?:\s*SET\b[^;]*;\s*)+`)

// limitStatementRe matches statements that can legally take a trailing LIMIT
// clause. Pinot only allows LIMIT on SELECT-shaped statements.
var limitStatementRe = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)

// unionOrOffsetRe matches statement shapes where appending LIMIT is invalid or
// would bind only the last UNION leg. EnforceLimit leaves these unchanged.
var unionOrOffsetRe = regexp.MustCompile(`(?i)(\bUNION\b|\bLIMIT\s+\d+\s+OFFSET\b|\bOFFSET\s+\d+\b)`)

// limitCommaRe matches Pinot's LIMIT offset, count form. The shared helper only
// sees LIMIT n at end-of-statement, so without a bail it would append a second
// LIMIT. The comma form already bounds the result; leave it as written.
var limitCommaRe = regexp.MustCompile(`(?i)\bLIMIT\s+\d+\s*,`)

// optionClauseRe matches a Pinot OPTION(...) hint. OPTION must be last, so a
// trailing LIMIT suffix is a syntax error. Enforcement skips and the command
// warns.
var optionClauseRe = regexp.MustCompile(`(?i)\bOPTION\s*\(`)

// trailingLineCommentRe matches a trailing `--` line comment. Appending
// "LIMIT n" after one would land inside the comment. hasTrailingComment
// strips quoted literals and identifiers first, so `SELECT '--' FROM t` does
// not match even though the raw SQL contains `--`.
var trailingLineCommentRe = regexp.MustCompile(`--[^\n]*$`)

func stripLeadingSetStatements(sql string) string {
	return leadingSetRe.ReplaceAllString(sql, "")
}

// limitBeforeTrailingCommentRe matches LIMIT n followed only by a trailing
// comment. Appending another LIMIT would produce invalid SQL.
var limitBeforeTrailingCommentRe = regexp.MustCompile(`(?is)\bLIMIT\s+\d+\s*(?:--[^\n]*|/\*.*?\*/)\s*$`)

// limitClauseRe matches internal/query/sql.EnforceLimit's trailing LIMIT n (digits
// immediately after LIMIT through end-of-statement). Comments between LIMIT and n
// are not matched; those statements must bail so a second LIMIT is not appended.
var limitClauseRe = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*$`)

var limitKeywordRe = regexp.MustCompile(`(?i)\bLIMIT\b`)

type lexicalMask struct {
	strings       bool
	quotedIdents  bool
	comments      bool
	blockComments bool
}

func blankRange(b []byte, start, end int) {
	for i := start; i < end; i++ {
		b[i] = ' '
	}
}

// maskLexical blanks string literals, quoted identifiers, and/or comments in
// scan order so `--` inside `/* */` and quotes inside comments are not misread.
func maskLexical(sql string, mask lexicalMask) string {
	out := []byte(sql)
	for i := 0; i < len(out); {
		switch {
		case mask.strings && out[i] == '\'':
			j := i + 1
			for j < len(out) {
				if out[j] == '\'' {
					if j+1 < len(out) && out[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			blankRange(out, i, j)
			i = j
		case mask.quotedIdents && out[i] == '"':
			j := i + 1
			for j < len(out) {
				if out[j] == '"' {
					if j+1 < len(out) && out[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			blankRange(out, i, j)
			i = j
		case mask.comments && i+1 < len(out) && out[i] == '-' && out[i+1] == '-':
			j := i
			for j < len(out) && out[j] != '\n' {
				j++
			}
			blankRange(out, i, j)
			i = j
		case (mask.comments || mask.blockComments) && i+1 < len(out) && out[i] == '/' && out[i+1] == '*':
			j := i + 2
			closed := false
			for j+1 < len(out) {
				if out[j] == '*' && out[j+1] == '/' {
					j += 2
					closed = true
					break
				}
				j++
			}
			if !closed {
				j = len(out)
			}
			blankRange(out, i, j)
			i = j
		default:
			i++
		}
	}
	return string(out)
}

func stripQuotedLiterals(sql string) string {
	return maskLexical(sql, lexicalMask{strings: true, quotedIdents: true})
}

// fromFuncRe matches Calcite functions whose FROM argument is not a table.
var fromFuncRe = regexp.MustCompile(`(?is)\b(?:EXTRACT|SUBSTRING|SUBSTR|TRIM|OVERLAY)\s*\([^)]*\)`)

func replaceSameLen(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
}

// maskNonTableFrom blanks regions where FROM is not a table clause, keeping
// the string the same length so FROM positions still index the parse copy.
// Quoted identifiers are blanked only on the search copy; the parse copy
// keeps them so FROM "my-table" still yields my-table.
func maskNonTableFrom(sql string, maskQuotedIdents bool) string {
	m := lexicalMask{strings: true, comments: true, quotedIdents: maskQuotedIdents}
	return replaceSameLen(fromFuncRe, maskLexical(sql, m))
}

// keywordScan removes literals, quoted identifiers, and comments so UNION /
// OPTION / LIMIT offset,count are matched only as real syntax.
func keywordScan(sql string) string {
	return maskLexical(sql, lexicalMask{strings: true, quotedIdents: true, comments: true})
}

func stripLeadingComments(sql string) string {
	s := sql
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if len(s) >= 2 && s[0] == '-' && s[1] == '-' {
			if idx := strings.IndexByte(s, '\n'); idx >= 0 {
				s = s[idx+1:]
				continue
			}
			return ""
		}
		if len(s) >= 2 && s[0] == '/' && s[1] == '*' {
			if end := strings.Index(s, "*/"); end >= 0 {
				s = s[end+2:]
				continue
			}
			return s
		}
		return s
	}
}

func limitStatementBody(sql string) string {
	s := stripLeadingComments(sql)
	s = stripLeadingSetStatements(s)
	return stripLeadingComments(s)
}

// hasUnclosedBlockComment reports an unclosed /* while skipping strings and
// line comments so apostrophes inside comments do not confuse the scan.
func hasUnclosedBlockComment(sql string) bool {
	out := []byte(sql)
	for i := 0; i < len(out); {
		switch {
		case out[i] == '\'':
			j := i + 1
			for j < len(out) {
				if out[j] == '\'' {
					if j+1 < len(out) && out[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			i = j
		case out[i] == '"':
			j := i + 1
			for j < len(out) {
				if out[j] == '"' {
					if j+1 < len(out) && out[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			i = j
		case i+1 < len(out) && out[i] == '-' && out[i+1] == '-':
			j := i
			for j < len(out) && out[j] != '\n' {
				j++
			}
			i = j
		case i+1 < len(out) && out[i] == '/' && out[i+1] == '*':
			j := i + 2
			closed := false
			for j+1 < len(out) {
				if out[j] == '*' && out[j+1] == '/' {
					j += 2
					closed = true
					break
				}
				j++
			}
			if !closed {
				return true
			}
			i = j
		default:
			i++
		}
	}
	return false
}

// hasLimitWeCannotRewrite reports a real LIMIT that is not a bare trailing
// LIMIT n (comments inside the clause, etc.). The shared helper would miss it
// and append a second LIMIT.
func hasLimitWeCannotRewrite(sql string) bool {
	trimmed := strings.TrimRight(sql, "; \t\n")
	if limitClauseRe.MatchString(trimmed) {
		return false
	}
	return limitKeywordRe.MatchString(keywordScan(sql))
}

// bail reports whether appending a trailing LIMIT would be invalid or unsafe.
func bail(sql string) bool {
	scanned := keywordScan(sql)
	if unionOrOffsetRe.MatchString(scanned) ||
		limitCommaRe.MatchString(scanned) ||
		optionClauseRe.MatchString(scanned) {
		return true
	}
	if hasTrailingLineComment(sql) {
		return true
	}
	if hasLimitBeforeTrailingComment(sql) {
		return true
	}
	if hasUnclosedBlockComment(sql) {
		return true
	}
	return hasLimitWeCannotRewrite(sql)
}

func hasLimitBeforeTrailingComment(sql string) bool {
	s := strings.TrimRight(stripQuotedLiterals(sql), "; \t\n")
	return limitBeforeTrailingCommentRe.MatchString(s)
}

// hasTrailingLineComment reports a -- line comment on the last line of the statement.
func hasTrailingLineComment(sql string) bool {
	trimmed := strings.TrimRight(sql, "; \t\n\r")
	line := trimmed
	if idx := strings.LastIndexByte(trimmed, '\n'); idx >= 0 {
		line = trimmed[idx+1:]
	}
	line = strings.TrimRight(
		maskLexical(line, lexicalMask{strings: true, quotedIdents: true, blockComments: true}),
		" \t\r",
	)
	return trailingLineCommentRe.MatchString(line)
}

// LimitFlagUsage is the --limit help text for the typed Pinot command.
func LimitFlagUsage(maxLimit int) string {
	return fmt.Sprintf("Max rows to return; requests above %d are capped (stderr notice when the query is adjusted). 0 disables enforcement", maxLimit)
}

// queryLimitInfo reports whether the SQL has a real LIMIT keyword and, when
// that clause is a clean trailing LIMIT n (after keywordScan blanks comments),
// the row cap n. Comma form (LIMIT offset,count), OFFSET, and non-numeric
// LIMIT return n == nil.
func queryLimitInfo(sql string) (*int, bool) {
	scanned := keywordScan(sql)
	if !limitKeywordRe.MatchString(scanned) {
		return nil, false
	}
	trimmed := strings.TrimRight(scanned, "; \t\n")
	m := limitClauseRe.FindStringSubmatch(trimmed)
	if m == nil {
		return nil, true
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, true
	}
	return &v, true
}

// LimitEnforcementNotice returns a short stderr message after EnforceLimit, or
// empty when no notice is needed. expr is the user's SQL; sql is what is sent.
// limitSet is true when the user passed --limit. A clean LIMIT n is read after
// keywordScan (LIMIT /* note */ 5000 yields 5000). Comma form is not a clean n.
func LimitEnforcementNotice(expr, sql string, capped bool, limit, maxLimit int, limitSet bool) string {
	if limit == 0 {
		return ""
	}

	existingN, hasLimit := queryLimitInfo(expr)

	if sql != expr {
		if capped {
			if limitSet && hasLimit {
				return fmt.Sprintf("You requested --limit %d but the existing LIMIT was above the maximum, so it was reduced to %d.", limit, maxLimit)
			}
			return fmt.Sprintf("Query adjusted: LIMIT reduced to %d (maximum). Use --limit 0 to disable enforcement.", maxLimit)
		}
		if !limitSet {
			return fmt.Sprintf("Query adjusted: appended default LIMIT %d. Use --limit 0 to disable enforcement.", limit)
		}
		return fmt.Sprintf("Query adjusted: appended LIMIT %d (--limit). Use --limit 0 to disable enforcement.", limit)
	}

	// Not SELECT/WITH after peeling leading comments and SET (EXPLAIN, INSERT, …): no notice.
	if !limitStatementRe.MatchString(limitStatementBody(expr)) {
		return ""
	}

	if hasLimit {
		if existingN != nil && *existingN > maxLimit {
			return fmt.Sprintf("The query has a LIMIT above %d that should be reduced to %d, but the query is not safe to modify.", maxLimit, maxLimit)
		}
		if limitSet && (existingN == nil || *existingN != limit) {
			return fmt.Sprintf("You requested --limit %d but the query already has a LIMIT, so no change was applied.", limit)
		}
		return ""
	}

	if limitSet && bail(expr) {
		return fmt.Sprintf("You asked for --limit %d, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0.", limit)
	}
	return ""
}

// EnforceLimit ensures the SQL has a LIMIT clause within bounds and reports
// whether an explicit trailing LIMIT was capped to maxLimit, so callers can
// warn instead of truncating silently.
// If limit is 0, enforcement is disabled (pass-through).
// SET prefixes are ignored for the SELECT-shaped allow-list. UNION, OFFSET,
// LIMIT offset,count, OPTION(...), and statements ending in a comment
// pass through unchanged. Keywords inside quotes or comments do not trigger a
// bail. Real DML never reaches bail: only SELECT/WITH do.
func EnforceLimit(sql string, limit, maxLimit int) (string, bool) {
	// Leave non-SELECT/WITH sql unchanged (after removing leading comments and SET).
	if !limitStatementRe.MatchString(limitStatementBody(sql)) {
		return sql, false
	}
	return querysql.EnforceLimit(sql, limit, maxLimit, bail)
}

var fromKeywordRe = regexp.MustCompile(`(?i)\bFROM\b`)

// One unquoted identifier segment. Dots are walked separately so
// "db"."events" and db."events" join the same way as my_db.events.
var identTableRe = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)`)

// ErrTableNameRequired is returned when StarTree's tableName field cannot be
// filled from SQL and the caller did not supply an override. The plugin
// rejects an empty tableName before it runs pinotQlCode.
var ErrTableNameRequired = errors.New("could not derive a table name from the SQL. StarTree needs one to load schema and expand macros before it runs the query. Pass --table <name> (a real table the query uses)")

// ExtractTableName returns the first FROM table we can be confident about, or
// empty if the shape is unclear. FROM inside strings, quoted identifiers,
// comments, and EXTRACT/TRIM/SUBSTRING/OVERLAY calls is ignored. FROM ( is
// skipped so a subquery or UNION wrapper yields the first inner table
// (SELECT … FROM (SELECT col FROM events) → events). An unclosed /* blanks
// only that tail, so a real FROM before it is still used. Used to fill
// StarTree's required tableName field; Pinot still executes pinotQlCode.
func ExtractTableName(sql string) string {
	search := maskNonTableFrom(sql, true)
	parse := maskNonTableFrom(sql, false)
	for _, loc := range fromKeywordRe.FindAllStringIndex(search, -1) {
		if name := tableNameAfterFrom(parse[loc[1]:]); name != "" {
			return name
		}
	}
	return ""
}

func tableNameAfterFrom(after string) string {
	rest := strings.TrimLeft(after, " \t\n\r")
	if rest == "" || rest[0] == '(' {
		return ""
	}
	part, rest, ok := tableIdentSegment(rest)
	if !ok {
		return ""
	}
	parts := []string{part}
	for {
		rest = strings.TrimLeft(rest, " \t\n\r")
		if rest == "" || rest[0] != '.' {
			break
		}
		rest = strings.TrimLeft(rest[1:], " \t\n\r")
		part, rest, ok = tableIdentSegment(rest)
		if !ok {
			return ""
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ".")
}

// parseQuotedIdent reads a SQL double-quoted identifier, unescaping "" to ".
func parseQuotedIdent(rest string) (string, string, bool) {
	if rest == "" || rest[0] != '"' {
		return "", rest, false
	}
	var b strings.Builder
	for i := 1; i < len(rest); i++ {
		switch rest[i] {
		case '"':
			if i+1 < len(rest) && rest[i+1] == '"' {
				b.WriteByte('"')
				i++
				continue
			}
			return b.String(), rest[i+1:], true
		default:
			b.WriteByte(rest[i])
		}
	}
	return "", rest, false
}

func tableIdentSegment(rest string) (string, string, bool) {
	if rest == "" {
		return "", rest, false
	}
	if rest[0] == '"' {
		return parseQuotedIdent(rest)
	}
	m := identTableRe.FindStringSubmatch(rest)
	if len(m) < 2 {
		return "", rest, false
	}
	return m[1], rest[len(m[0]):], true
}

// ResolveTableName returns override when set, otherwise ExtractTableName(sql).
// Empty after both is ErrTableNameRequired: StarTree will not run the query.
func ResolveTableName(sql, override string) (string, error) {
	if t := strings.TrimSpace(override); t != "" {
		return t, nil
	}
	if t := ExtractTableName(sql); t != "" {
		return t, nil
	}
	return "", ErrTableNameRequired
}

// QueryRequest represents a PinotQL query request.
type QueryRequest struct {
	RawSQL string
	// TableName fills StarTree's required editor field. Empty means derive
	// it from the first confident FROM in RawSQL (including inside FROM ().
	// SQL with no extractable table must set this (or the CLI --table flag).
	TableName  string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}
