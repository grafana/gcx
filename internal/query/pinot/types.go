package pinot

import (
	"errors"
	"fmt"
	"regexp"
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

	// LimitSkipShapes is the user-facing list of SQL shapes where EnforceLimit
	// leaves the statement unchanged and callers warn. Flag help, stderr
	// warnings, and LimitNotEnforced must all use this string.
	LimitSkipShapes = "UNION, OFFSET, OPTION, or a trailing comment"
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

// trailingLineCommentRe matches a real `--` line comment (whitespace or start
// before `--`) that runs to the end of the statement. Appending "LIMIT n"
// after one would land inside the comment. A `--` inside a string literal
// (`SELECT '--' FROM t`) does not match.
var trailingLineCommentRe = regexp.MustCompile(`(^|[\s;])--[^\n]*$`)

func selectBody(sql string) string {
	return leadingSetRe.ReplaceAllString(sql, "")
}

// sqlStringRe matches a single-quoted SQL literal, including escaped quotes.
var sqlStringRe = regexp.MustCompile(`'([^']|'')*'`)

// sqlQuotedIdentRe matches a double-quoted identifier, including escaped quotes.
var sqlQuotedIdentRe = regexp.MustCompile(`"([^"]|"")*"`)

var sqlLineCommentRe = regexp.MustCompile(`--[^\n]*`)

var sqlBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

var sqlUnclosedBlockRe = regexp.MustCompile(`(?s)/\*.*$`)

func stripQuoted(sql string) string {
	s := sqlStringRe.ReplaceAllString(sql, " ")
	return sqlQuotedIdentRe.ReplaceAllString(s, " ")
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
	s := replaceSameLen(sqlStringRe, sql)
	if maskQuotedIdents {
		s = replaceSameLen(sqlQuotedIdentRe, s)
	}
	s = replaceSameLen(sqlLineCommentRe, s)
	s = replaceSameLen(sqlBlockCommentRe, s)
	s = replaceSameLen(sqlUnclosedBlockRe, s)
	return replaceSameLen(fromFuncRe, s)
}

// keywordScan removes literals, quoted identifiers, and comments so UNION /
// OPTION / LIMIT offset,count are matched only as real syntax.
func keywordScan(sql string) string {
	s := stripQuoted(sql)
	s = sqlLineCommentRe.ReplaceAllString(s, " ")
	return sqlBlockCommentRe.ReplaceAllString(s, " ")
}

func hasTrailingComment(sql string) bool {
	s := strings.TrimRight(stripQuoted(sql), "; \t\n")
	if trailingLineCommentRe.MatchString(s) {
		return true
	}
	return sqlUnclosedBlockRe.MatchString(sqlBlockCommentRe.ReplaceAllString(s, " "))
}

func bail(sql string) bool {
	if hasTrailingComment(sql) {
		return true
	}
	s := keywordScan(sql)
	return unionOrOffsetRe.MatchString(s) || limitCommaRe.MatchString(s) || optionClauseRe.MatchString(s)
}

// LimitFlagUsage is the --limit help text for the typed Pinot command.
func LimitFlagUsage(maxLimit int) string {
	return fmt.Sprintf("Max rows to return; requests above %d are capped, with a warning. Not applied to %s (warned on stderr). 0 disables enforcement", maxLimit, LimitSkipShapes)
}

// LimitCappedWarning is the stderr notice when a LIMIT was reduced to maxLimit.
func LimitCappedWarning(maxLimit int) string {
	return fmt.Sprintf("LIMIT in query exceeds the maximum of %d and was capped; use --limit 0 to disable enforcement", maxLimit)
}

// LimitSkipWarning is the stderr notice when EnforceLimit left SQL unchanged.
func LimitSkipWarning() string {
	return fmt.Sprintf("query uses %s, so --limit was not applied; the SQL was sent unchanged. Use --limit 0 to disable this warning", LimitSkipShapes)
}

// LimitWarning returns the stderr notice for a limit rewrite, or empty.
func LimitWarning(expr string, capped bool, limit, maxLimit int) string {
	if capped {
		return LimitCappedWarning(maxLimit)
	}
	if limit != 0 && LimitNotEnforced(expr) {
		return LimitSkipWarning()
	}
	return ""
}

// LimitNotEnforced reports whether EnforceLimit will leave a SELECT/WITH
// statement unchanged for a reason the user should hear about: the shapes in
// LimitSkipShapes. LIMIT offset,count is excluded because that form already
// bounds the result. Keywords inside quotes or comments do not count.
func LimitNotEnforced(sql string) bool {
	if !limitStatementRe.MatchString(selectBody(sql)) {
		return false
	}
	if limitCommaRe.MatchString(keywordScan(sql)) {
		return false
	}
	return bail(sql)
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
	if !limitStatementRe.MatchString(selectBody(sql)) {
		return sql, false
	}
	return querysql.EnforceLimit(sql, limit, maxLimit, bail)
}

var fromKeywordRe = regexp.MustCompile(`(?i)\bFROM\b`)

var quotedTableRe = regexp.MustCompile(`^"([^"]+)"`)

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

func tableIdentSegment(rest string) (string, string, bool) {
	if rest == "" {
		return "", rest, false
	}
	if rest[0] == '"' {
		m := quotedTableRe.FindStringSubmatch(rest)
		if len(m) < 2 {
			return "", rest, false
		}
		return m[1], rest[len(m[0]):], true
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
