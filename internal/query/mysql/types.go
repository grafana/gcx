package mysql

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

// EscapeSQLString escapes single quotes for use in SQL string literals.
// Backticks and other quoting characters need no handling here: every value
// reaching this function is pre-validated by ValidateIdentifier, which
// already rejects them.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateIdentifier checks that a single database or table name contains
// only safe characters. Database-qualified names (db.table) must be split
// before validation — a dot inside one identifier would silently match
// nothing in information_schema lookups.
func ValidateIdentifier(name, field string) error {
	if name == "" {
		return nil
	}
	if !identifierRe.MatchString(name) {
		return fmt.Errorf("invalid %s: must contain only letters, numbers, and underscores", field)
	}
	return nil
}

// limitStatementRe matches statements that should get a trailing LIMIT
// clause. MySQL accepts LIMIT on single-table UPDATE/DELETE and on
// INSERT ... SELECT — appending one there would silently restrict the write
// to LIMIT rows, so only SELECT-shaped statements are ever touched.
var limitStatementRe = regexp.MustCompile(`(?is)^\s*(SELECT|WITH|TABLE|VALUES)\b`)

// The DML keywords catch CTE-wrapped writes (e.g. WITH ... UPDATE ...),
// which start with WITH and so pass limitStatementRe. Matching them anywhere
// means a SELECT mentioning e.g. 'DELETE' in a string literal also skips
// enforcement — that fails safe (no LIMIT added) rather than limiting a write.
// EXPLAIN/SHOW/DESCRIBE need no bail entries: limitStatementRe already
// excludes them, and line-anchored entries would misfire on formatted
// queries (e.g. a multiline ORDER BY ... \nDESC).
//
// REPLACE is deliberately not in this alternation: unlike INSERT/UPDATE/
// DELETE, REPLACE is also MySQL's most common string function, and a bare
// \b match can't tell REPLACE INTO t ... apart from SELECT REPLACE(col,'a',
// 'b') FROM t. isReplaceWrite below resolves that ambiguity by position
// instead.
var limitBailRe = regexp.MustCompile(`(?i)(\bLIMIT\s+\d+\s+OFFSET\b|\bLIMIT\s+\d+\s*,|\bOFFSET\s+\d+\b|\bINTO\s+(OUTFILE|DUMPFILE)\b|\bFOR\s+(UPDATE|SHARE)\b|\bLOCK\s+IN\s+SHARE\s+MODE\b|\b(INSERT|UPDATE|DELETE)\b)`)

var replaceKeywordRe = regexp.MustCompile(`(?i)\bREPLACE\b`)

// isReplaceWrite reports whether sql contains REPLACE used as a write
// statement (REPLACE INTO ..., REPLACE tbl_name SET ...) rather than a call
// to the REPLACE(str, from, to) string function. Go's RE2 engine has no
// lookahead, so telling the two apart takes a second pass: a write's
// REPLACE is never immediately followed by "(", a function call's always
// is, so any occurrence that isn't followed by "(" is a write.
func isReplaceWrite(sql string) bool {
	for _, loc := range replaceKeywordRe.FindAllStringIndex(sql, -1) {
		rest := strings.TrimLeft(sql[loc[1]:], " \t\r\n")
		if !strings.HasPrefix(rest, "(") {
			return true
		}
	}
	return false
}

// trailingLineCommentRe matches a MySQL line comment (`-- ` or `#`) that
// runs to the end of the statement. Appending "LIMIT n" as a bare suffix
// after one would land inside the comment and be silently dropped, leaving
// the query unbounded. MySQL requires whitespace after "--" for it to start
// a comment (bare "--" is double unary minus), but no such requirement for
// "#".
var trailingLineCommentRe = regexp.MustCompile(`(--[ \t][^\n]*|#[^\n]*)$`)

func bail(sql string) bool {
	trimmed := strings.TrimRight(sql, "; \t\n")
	return limitBailRe.MatchString(sql) || isReplaceWrite(sql) || trailingLineCommentRe.MatchString(trimmed)
}

// EnforceLimit ensures the SQL has a LIMIT clause within bounds and reports
// whether an explicit trailing LIMIT was capped to maxLimit, so callers can
// warn instead of truncating silently.
// If limit is 0, enforcement is disabled (pass-through).
// Statements that cannot safely take a trailing LIMIT (DML, DDL,
// EXPLAIN/SHOW/DESCRIBE, ...), SELECTs using OFFSET, LIMIT offset,count,
// INTO OUTFILE, or row locking, and statements ending in a line comment all
// pass through unchanged.
func EnforceLimit(sql string, limit, maxLimit int) (string, bool) {
	if !limitStatementRe.MatchString(sql) {
		return sql, false
	}
	return querysql.EnforceLimit(sql, limit, maxLimit, bail)
}

// QueryRequest represents a MySQL query request.
type QueryRequest struct {
	RawSQL     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}
