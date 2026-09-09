package pinot

import (
	"errors"
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

const (
	// DatasourceType is the Grafana plugin ID for StarTree Pinot datasources.
	DatasourceType = "startree-pinot-datasource"
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

func bail(sql string) bool {
	return unionOrOffsetRe.MatchString(sql) || limitCommaRe.MatchString(sql) || optionClauseRe.MatchString(sql) || trailingLineCommentRe.MatchString(strings.TrimRight(sql, "; \t\n"))
}

// LimitNotEnforced reports whether EnforceLimit will leave sql unchanged
// because a LIMIT suffix would bind only the last UNION leg, collide with
// OFFSET, or land after OPTION. LIMIT offset,count is not included: that form
// already bounds the result, so it is left alone without a warning.
func LimitNotEnforced(sql string) bool {
	if !limitStatementRe.MatchString(selectBody(sql)) {
		return false
	}
	return unionOrOffsetRe.MatchString(sql) || optionClauseRe.MatchString(sql)
}

// EnforceLimit ensures the SQL has a LIMIT clause within bounds and reports
// whether an explicit trailing LIMIT was capped to maxLimit, so callers can
// warn instead of truncating silently.
// If limit is 0, enforcement is disabled (pass-through).
// SET prefixes are ignored for the SELECT-shaped allow-list. UNION, OFFSET,
// LIMIT offset,count, OPTION(...), and statements ending in a line comment
// pass through unchanged. Real DML never reaches bail: only SELECT/WITH do.
func EnforceLimit(sql string, limit, maxLimit int) (string, bool) {
	if !limitStatementRe.MatchString(selectBody(sql)) {
		return sql, false
	}
	return querysql.EnforceLimit(sql, limit, maxLimit, bail)
}

// sqlStringRe matches a single-quoted SQL literal, including escaped quotes.
var sqlStringRe = regexp.MustCompile(`'([^']|'')*'`)

// extractCallRe matches EXTRACT(...) so a FROM inside the call is not treated
// as a table (e.g. EXTRACT(YEAR FROM ts)).
var extractCallRe = regexp.MustCompile(`(?is)\bEXTRACT\s*\([^)]*\)`)

var fromKeywordRe = regexp.MustCompile(`(?i)\bFROM\b`)

var quotedTableRe = regexp.MustCompile(`^"([^"]+)"`)

var identTableRe = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_.]*)`)

// ErrTableNameRequired is returned when StarTree's tableName field cannot be
// filled from SQL and the caller did not supply an override. The plugin
// rejects an empty tableName before it runs pinotQlCode.
var ErrTableNameRequired = errors.New("could not derive a table name from the SQL. StarTree needs one to load schema and expand macros before it runs the query. Pass --table <name> (a real table the query uses)")

// ExtractTableName returns the first FROM table we can be confident about, or
// empty if the shape is unclear. String literals and EXTRACT(...) are stripped
// first. FROM ( is skipped so a subquery or UNION wrapper yields the first
// inner table (SELECT … FROM (SELECT col FROM events) → events). Used to fill
// StarTree's required tableName field; Pinot still executes pinotQlCode.
func ExtractTableName(sql string) string {
	s := sqlStringRe.ReplaceAllString(sql, " ")
	s = extractCallRe.ReplaceAllString(s, " ")
	for _, loc := range fromKeywordRe.FindAllStringIndex(s, -1) {
		if name := tableNameAfterFrom(s[loc[1]:]); name != "" {
			return name
		}
	}
	return ""
}

func tableNameAfterFrom(after string) string {
	rest := strings.TrimLeft(after, " \t\n")
	if rest == "" || rest[0] == '(' {
		return ""
	}
	if rest[0] == '"' {
		m := quotedTableRe.FindStringSubmatch(rest)
		if len(m) < 2 {
			return ""
		}
		return m[1]
	}
	m := identTableRe.FindStringSubmatch(rest)
	if len(m) < 2 {
		return ""
	}
	return m[1]
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
