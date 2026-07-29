package mssql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DatasourceType is the Grafana plugin ID for Microsoft SQL Server.
	DatasourceType = "mssql"
	// QueryFormatTable selects tabular output. Unlike the sqlds-based datasources
	// (ClickHouse, Athena) which use an integer format code, the core MSSQL plugin
	// expects the string "table"; sending an integer makes the plugin fail with
	// HTTP 500 ("An error occurred within the plugin").
	QueryFormatTable = "table"
)

// QueryRequest represents an MSSQL query request.
type QueryRequest struct {
	RawSQL string
	Start  time.Time
	End    time.Time
}

// EscapeSQLString escapes single quotes for use in a T-SQL string literal.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// SplitSchemaQualifiedTable splits a possibly schema-qualified table reference
// (e.g. "dbo.WORLD_DATA") into its schema and table parts, returning
// (schema, table, error). A bare name ("WORLD_DATA") returns an empty schema.
// It errors on more than two dot-separated parts (e.g. db.schema.table), which
// gcx cannot map onto INFORMATION_SCHEMA's two-level schema/table columns.
// Individual parts are not validated here — callers pass them through
// ValidateIdentifier.
func SplitSchemaQualifiedTable(name string) (string, string, error) {
	if !strings.Contains(name, ".") {
		return "", name, nil
	}
	parts := strings.Split(name, ".")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid table %q: use SCHEMA.TABLE (e.g. dbo.WORLD_DATA) or pass --schema", name)
	}
	return parts[0], parts[1], nil
}

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateIdentifier checks that a schema or table name contains only safe
// characters. Schema and table parts are validated individually, so dots are
// not permitted.
func ValidateIdentifier(name, field string) error {
	if name == "" {
		return nil
	}
	if !identifierRe.MatchString(name) {
		return fmt.Errorf("invalid %s: must contain only letters, numbers, and underscores", field)
	}
	return nil
}

var (
	leadingSelectRe = regexp.MustCompile(`(?is)^\s*SELECT\s+(?:DISTINCT\s+|ALL\s+)?`)
	existingTopRe   = regexp.MustCompile(`(?is)^\s*SELECT\s+(?:DISTINCT\s+|ALL\s+)?TOP\b`)
	setOpRe         = regexp.MustCompile(`(?is)\b(?:UNION|INTERSECT|EXCEPT)\b`)
	offsetRe        = regexp.MustCompile(`(?is)\bOFFSET\b`)
)

// EnforceTop injects a `TOP (n)` clause into a simple leading-SELECT query to
// cap the row count. T-SQL has no LIMIT keyword, so this is the MSSQL equivalent
// of the other SQL datasources' EnforceLimit. If limit is 0, injection is
// disabled (pass-through). limit is clamped to maxLimit.
//
// It bails (returns the SQL unchanged) whenever injecting TOP would be invalid
// or change semantics: non-SELECT statements, CTEs (WITH ...), queries that
// already use TOP, set operations (UNION/INTERSECT/EXCEPT), and paged queries
// (OFFSET ... FETCH). Bailing is always safe because it only forgoes the cap.
//
// Note the deliberate divergence from the shared EnforceLimit: when the SQL
// already carries a cap, EnforceLimit rewrites an over-max `LIMIT n` down to
// maxLimit, but EnforceTop leaves an existing TOP untouched rather than clamping
// it. Rewriting TOP is not a safe find-and-replace — it has `TOP (n) PERCENT`
// and `TOP (n) WITH TIES` variants whose semantics a naive clamp would corrupt —
// so a user-supplied TOP is always honoured as-is.
func EnforceTop(sql string, limit, maxLimit int) string {
	if limit == 0 {
		return sql
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	loc := leadingSelectRe.FindStringIndex(sql)
	if loc == nil {
		return sql // not a leading-SELECT statement (e.g. WITH, EXEC, INSERT)
	}
	if existingTopRe.MatchString(sql) || setOpRe.MatchString(sql) || offsetRe.MatchString(sql) {
		return sql
	}

	// Insert `TOP (n) ` immediately after the leading SELECT [DISTINCT|ALL].
	insertAt := loc[1]
	return sql[:insertAt] + "TOP (" + strconv.Itoa(limit) + ") " + sql[insertAt:]
}
