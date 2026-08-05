package bigquery

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

// DatasourceType is the Grafana plugin ID for BigQuery datasources.
const DatasourceType = "grafana-bigquery-datasource"

// EscapeSQLString escapes single quotes for use in SQL string literals.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

var (
	// nameRe matches BigQuery dataset and table identifiers: letters, numbers,
	// and underscores.
	nameRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	// projectRe matches BigQuery project IDs, which also allow hyphens
	// (e.g. "partner-datasources").
	projectRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ValidateName checks that a dataset or table identifier contains only safe
// characters. An empty name is allowed (callers enforce required-ness).
func ValidateName(name, field string) error {
	if name == "" {
		return nil
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid %s: must contain only letters, numbers, and underscores", field)
	}
	return nil
}

// ValidateProject checks that a project identifier contains only safe
// characters. An empty project is allowed (the datasource's default project is
// used). Projects may contain hyphens, unlike dataset/table names.
func ValidateProject(project string) error {
	if project == "" {
		return nil
	}
	if !projectRe.MatchString(project) {
		return errors.New("invalid project: must contain only letters, numbers, underscores, and hyphens")
	}
	return nil
}

// SplitQualifiedTable splits a possibly qualified table reference into its
// project, dataset, and table parts, returning (project, dataset, table, error):
//
//	"WORLD_DATA"                       -> ("", "", "WORLD_DATA")
//	"my_dataset.WORLD_DATA"            -> ("", "my_dataset", "WORLD_DATA")
//	"my-project.my_dataset.WORLD_DATA" -> ("my-project", "my_dataset", "WORLD_DATA")
//
// It errors on more than three dot-separated parts. Parts are not validated
// here — callers pass them through ValidateProject / ValidateName.
func SplitQualifiedTable(name string) (string, string, string, error) {
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 1:
		return "", "", parts[0], nil
	case 2:
		return "", parts[0], parts[1], nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	default:
		return "", "", "", fmt.Errorf("invalid table %q: use TABLE, DATASET.TABLE, or PROJECT.DATASET.TABLE (or pass --project/--dataset)", name)
	}
}

// InfoSchemaPrefix builds a backtick-quoted INFORMATION_SCHEMA path prefix from
// an optional project and dataset. Callers MUST validate the identifiers first
// (see ValidateProject / ValidateName) so interpolation is safe.
//
//	project="p", dataset="d" -> "`p.d`.INFORMATION_SCHEMA"
//	project="",  dataset="d" -> "`d`.INFORMATION_SCHEMA"
//	project="p", dataset=""  -> "`p`.INFORMATION_SCHEMA"
//	project="",  dataset=""  -> "INFORMATION_SCHEMA"
func InfoSchemaPrefix(project, dataset string) string {
	parts := make([]string, 0, 2)
	if project != "" {
		parts = append(parts, project)
	}
	if dataset != "" {
		parts = append(parts, dataset)
	}
	if len(parts) == 0 {
		return "INFORMATION_SCHEMA"
	}
	return fmt.Sprintf("`%s`.INFORMATION_SCHEMA", strings.Join(parts, "."))
}

// limitBailRe matches statements where appending a LIMIT would be invalid or
// meaningless (DDL, metadata statements, or a query that already paginates via
// LIMIT ... OFFSET).
//
// The statement-leading keywords are anchored with \A so they only match at the
// start of the whole statement. Do NOT use the (?m) flag here: it would make ^
// match at every line start, so a multi-line query such as
// "SELECT … \nORDER BY ts\nDESC" would silently bail out and lose its row cap.
// The LIMIT … OFFSET branch is deliberately unanchored — it must match anywhere.
var limitBailRe = regexp.MustCompile(`(?i)(\bLIMIT\s+\d+\s+OFFSET\b|\A\s*(?:EXPLAIN|DESC(?:RIBE)?|SHOW|CREATE|DROP|ALTER)\b)`)

// EnforceLimit ensures the SQL has a LIMIT clause within bounds.
// If limit is 0, enforcement is disabled (pass-through).
// If the SQL is a DDL/metadata statement or already uses LIMIT ... OFFSET, it
// bails out (pass-through).
func EnforceLimit(sql string, limit, maxLimit int) string {
	return querysql.EnforceLimit(sql, limit, maxLimit, limitBailRe.MatchString)
}

// QueryRequest represents a BigQuery query request.
type QueryRequest struct {
	RawSQL string
	Start  time.Time
	End    time.Time
}

// StringList is a single-column discovery result (e.g. dataset names) with a
// header for table rendering.
type StringList struct {
	Items  []string `json:"items"`
	Header string   `json:"-"`
}

// TableInfo represents a row from INFORMATION_SCHEMA.TABLES.
type TableInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ColumnInfo represents a row from INFORMATION_SCHEMA.COLUMNS.
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
}

// ParseStringColumn extracts the first column of a QueryResponse as a slice of
// strings (used for single-column discovery queries such as SCHEMATA).
func ParseStringColumn(resp *querysql.QueryResponse) []string {
	items := make([]string, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		if len(row) < 1 {
			continue
		}
		items = append(items, fmt.Sprint(row[0]))
	}
	return items
}

// ParseTableInfoRows converts a QueryResponse from the TABLES query into typed
// TableInfo values.
func ParseTableInfoRows(resp *querysql.QueryResponse) []TableInfo {
	tables := make([]TableInfo, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		if len(row) < 2 {
			continue
		}
		tables = append(tables, TableInfo{
			Name: fmt.Sprint(row[0]),
			Type: fmt.Sprint(row[1]),
		})
	}
	return tables
}

// ParseColumnInfoRows converts a QueryResponse from the COLUMNS query into typed
// ColumnInfo values.
func ParseColumnInfoRows(resp *querysql.QueryResponse) []ColumnInfo {
	cols := make([]ColumnInfo, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		if len(row) < 3 {
			continue
		}
		cols = append(cols, ColumnInfo{
			Name:     fmt.Sprint(row[0]),
			Type:     fmt.Sprint(row[1]),
			Nullable: fmt.Sprint(row[2]),
		})
	}
	return cols
}
