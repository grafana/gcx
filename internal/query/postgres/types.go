package postgres

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

// EscapeSQLString escapes single quotes for use in SQL string literals.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateIdentifier checks that a single schema or table name contains only
// safe characters. Schema-qualified names (schema.table) must be split before
// validation — a dot inside one identifier would silently match nothing in
// information_schema lookups.
func ValidateIdentifier(name, field string) error {
	if name == "" {
		return nil
	}
	if !identifierRe.MatchString(name) {
		return fmt.Errorf("invalid %s: must contain only letters, numbers, and underscores", field)
	}
	return nil
}

// limitStatementRe matches statements that can legally take a trailing LIMIT
// clause. Postgres only allows LIMIT on SELECT-shaped statements — appending
// it to DML/DDL/utility statements would produce a syntax error.
var limitStatementRe = regexp.MustCompile(`(?is)^\s*(SELECT|WITH|TABLE|VALUES)\b`)

// The DML keywords catch CTE-wrapped writes (e.g. WITH ... DELETE FROM ...),
// which start with WITH and so pass limitStatementRe. Matching them anywhere
// means a SELECT mentioning e.g. 'DELETE' in a string literal also skips
// enforcement — that fails safe (no LIMIT added) rather than corrupting SQL.
// EXPLAIN/SHOW need no bail entries: limitStatementRe already excludes them,
// and line-anchored entries would misfire on formatted multiline queries.
// LIMIT ALL is included because querysql's rewrite only recognizes a numeric
// trailing LIMIT — leaving "LIMIT ALL" unmatched would double up into
// "LIMIT ALL LIMIT 100" instead of bailing.
var limitBailRe = regexp.MustCompile(`(?i)(\bLIMIT\s+\d+\s+OFFSET\b|\bOFFSET\s+\d+\b|\bFETCH\s+(FIRST|NEXT)\b|\bRETURNING\b|\bFOR\s+(UPDATE|SHARE)\b|\b(INSERT|UPDATE|DELETE|MERGE)\b|\bLIMIT\s+ALL\b)`)

// trailingLineCommentRe matches a `--` line comment that runs to the end of
// the statement. Appending "LIMIT n" as a bare suffix after one would land
// inside the comment and be dropped, silently leaving the query unbounded.
var trailingLineCommentRe = regexp.MustCompile(`--[^\n]*$`)

func bail(sql string) bool {
	return limitBailRe.MatchString(sql) || trailingLineCommentRe.MatchString(strings.TrimRight(sql, "; \t\n"))
}

// EnforceLimit ensures the SQL has a LIMIT clause within bounds and reports
// whether an explicit trailing LIMIT was capped to maxLimit, so callers can
// warn instead of truncating silently.
// If limit is 0, enforcement is disabled (pass-through).
// Statements that cannot take LIMIT (DML, DDL, EXPLAIN/SHOW, ...), SELECTs
// using OFFSET, FETCH, RETURNING, row locking, or an existing LIMIT ALL, and
// statements ending in a line comment all pass through unchanged for the
// server to validate.
func EnforceLimit(sql string, limit, maxLimit int) (string, bool) {
	if !limitStatementRe.MatchString(sql) {
		return sql, false
	}
	return querysql.EnforceLimit(sql, limit, maxLimit, bail)
}

// QueryRequest represents a PostgreSQL query request.
type QueryRequest struct {
	RawSQL     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}
