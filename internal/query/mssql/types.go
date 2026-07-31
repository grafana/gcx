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
	// IntervalMs sets the query interval the plugin uses to resolve the
	// $__interval / $__timeGroup(col, $__interval) macros. Zero omits it, so the
	// plugin falls back to its own default.
	IntervalMs int64
}

// EscapeSQLString escapes single quotes for use in a T-SQL string literal.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// SplitSchemaQualifiedTable splits a possibly schema-qualified table reference
// (e.g. "dbo.WORLD_DATA") into its schema and table parts, returning
// (schema, table, error). A bare name ("WORLD_DATA") returns an empty schema.
// It errors on more than two dot-separated parts (e.g. db.schema.table), which
// gcx cannot map onto INFORMATION_SCHEMA's two-level schema/table columns, and
// on an empty schema or table part (e.g. "dbo." or ".WORLD_DATA"), which is
// almost always a typo. Individual parts are not otherwise validated here —
// callers pass them through ValidateIdentifier.
func SplitSchemaQualifiedTable(name string) (string, string, error) {
	if !strings.Contains(name, ".") {
		return "", name, nil
	}
	parts := strings.Split(name, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
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
// of the other SQL datasources' EnforceLimit. If limit is 0 or negative,
// injection is disabled (pass-through). limit is clamped to maxLimit.
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
	if limit <= 0 {
		return sql
	}
	out, _ := injectTop(sql, min(limit, maxLimit))
	return out
}

// EnforceTopSentinel is EnforceTop's truncation-detecting variant. It injects
// `TOP (eff+1)` (eff = min(limit, maxLimit)) so the caller can tell whether more
// rows matched than the cap allows, and returns the SQL to execute, the
// effective row cap to display (eff), and whether gcx injected a fresh TOP.
// When capped is true, run the query and call (*QueryResponse).Truncate(eff);
// if it reports dropped rows, warn with querysql.TruncationHint. When capped is
// false (limit disabled, or the statement was left unchanged) the caller must
// not truncate or warn. Mirrors querysql.EnforceLimitSentinel for the LIMIT
// datasources.
func EnforceTopSentinel(sql string, limit, maxLimit int) (string, int, bool) {
	if limit <= 0 {
		return sql, 0, false
	}
	eff := min(limit, maxLimit)
	out, capped := injectTop(sql, eff+1)
	return out, eff, capped
}

// injectTop inserts `TOP (n)` immediately after a leading SELECT [DISTINCT|ALL],
// returning the rewritten SQL and whether the injection happened. It bails
// (returns the SQL unchanged, false) for statements where injecting TOP would be
// invalid or change semantics: non-SELECT statements, CTEs (WITH ...), queries
// that already use TOP, set operations, and paged (OFFSET ... FETCH) queries.
func injectTop(sql string, n int) (string, bool) {
	loc := leadingSelectRe.FindStringIndex(sql)
	if loc == nil {
		return sql, false // not a leading-SELECT statement (e.g. WITH, EXEC, INSERT)
	}
	if existingTopRe.MatchString(sql) || setOpRe.MatchString(sql) || offsetRe.MatchString(sql) {
		return sql, false
	}

	insertAt := loc[1]
	return sql[:insertAt] + "TOP (" + strconv.Itoa(n) + ") " + sql[insertAt:], true
}
