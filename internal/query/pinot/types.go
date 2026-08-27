package pinot

import (
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

const (
	// DatasourceType is the Grafana plugin ID for StarTree Pinot datasources.
	DatasourceType = "startree-pinot-datasource"
)

// EscapeSQLString escapes single quotes for use in SQL string literals.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// leadingSetRe matches one or more Pinot SET statements at the start of a
// query (e.g. SET useMultistageEngine = true;). Those prefixes are stripped
// before the SELECT-shaped allow-list so EnforceLimit can still bound the
// SELECT that follows.
var leadingSetRe = regexp.MustCompile(`(?is)^(?:\s*SET\b[^;]*;\s*)+`)

// limitStatementRe matches statements that can legally take a trailing LIMIT
// clause. Pinot only allows LIMIT on SELECT-shaped statements.
var limitStatementRe = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)

// UNION bails so a trailing LIMIT on a multi-leg query is left alone — Faro
// user-journey dumps already carry LIMIT 2000 and must not be capped silently.
// OFFSET / LIMIT OFFSET would double up into a syntax error. DML keywords
// anywhere fail safe (no LIMIT added) rather than corrupting a write. A
// SELECT mentioning those words in a string literal also skips enforcement.
var limitBailRe = regexp.MustCompile(`(?i)(\bUNION\b|\bLIMIT\s+\d+\s+OFFSET\b|\bOFFSET\s+\d+\b|\b(INSERT|UPDATE|DELETE|MERGE)\b)`)

// trailingLineCommentRe matches a `--` line comment that runs to the end of
// the statement. Appending "LIMIT n" as a bare suffix after one would land
// inside the comment and be dropped, silently leaving the query unbounded.
var trailingLineCommentRe = regexp.MustCompile(`--[^\n]*$`)

func selectBody(sql string) string {
	return leadingSetRe.ReplaceAllString(sql, "")
}

func bail(sql string) bool {
	return limitBailRe.MatchString(sql) || trailingLineCommentRe.MatchString(strings.TrimRight(sql, "; \t\n"))
}

// EnforceLimit ensures the SQL has a LIMIT clause within bounds and reports
// whether an explicit trailing LIMIT was capped to maxLimit, so callers can
// warn instead of truncating silently.
// If limit is 0, enforcement is disabled (pass-through).
// SET prefixes are ignored for the SELECT-shaped allow-list. UNION, OFFSET,
// DML, and statements ending in a line comment pass through unchanged.
func EnforceLimit(sql string, limit, maxLimit int) (string, bool) {
	if !limitStatementRe.MatchString(selectBody(sql)) {
		return sql, false
	}
	return querysql.EnforceLimit(sql, limit, maxLimit, bail)
}

// fromTableRe captures the first unquoted or double-quoted table after FROM.
// Subqueries (`FROM (SELECT ...)`) do not match. Used only to fill StarTree's
// tableName editor field; Pinot executes pinotQlCode regardless.
var fromTableRe = regexp.MustCompile(`(?is)\bFROM\s+"?([a-zA-Z_][a-zA-Z0-9_.]*)"?`)

// ExtractTableName returns the first FROM table in sql, or empty if none.
func ExtractTableName(sql string) string {
	m := fromTableRe.FindStringSubmatch(sql)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// QueryRequest represents a PinotQL query request.
type QueryRequest struct {
	RawSQL     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}
