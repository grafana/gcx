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

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// ValidateIdentifier checks that a schema or table name contains only safe characters.
func ValidateIdentifier(name, field string) error {
	if name == "" {
		return nil
	}
	if !identifierRe.MatchString(name) {
		return fmt.Errorf("invalid %s: must contain only letters, numbers, underscores, and dots", field)
	}
	return nil
}

var limitBailRe = regexp.MustCompile(`(?im)(\bLIMIT\s+\d+\s+OFFSET\b|\bOFFSET\s+\d+\b|\bFETCH\s+(FIRST|NEXT)\b|\bRETURNING\b|\bFOR\s+(UPDATE|SHARE)\b|^\s*EXPLAIN\b|^\s*SHOW\b)`)

// EnforceLimit ensures the SQL has a LIMIT clause within bounds.
// If limit is 0, enforcement is disabled (pass-through).
// If the SQL uses OFFSET, FETCH, RETURNING, row locking, or a metadata
// statement (EXPLAIN/SHOW), it bails out (pass-through).
func EnforceLimit(sql string, limit, maxLimit int) string {
	return querysql.EnforceLimit(sql, limit, maxLimit, limitBailRe.MatchString)
}

// QueryRequest represents a PostgreSQL query request.
type QueryRequest struct {
	RawSQL     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}
