package clickhouse

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

// EscapeSQLString escapes single quotes for use in SQL string literals.
// Matches the ClickHouse plugin's own escapeSQLString implementation.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// ValidateIdentifier checks that a database or table name contains only safe characters.
func ValidateIdentifier(name, field string) error {
	if name == "" {
		return nil
	}
	if !identifierRe.MatchString(name) {
		return fmt.Errorf("invalid %s: must contain only letters, numbers, underscores, and dots", field)
	}
	return nil
}

var limitBailRe = regexp.MustCompile(`(?im)(\bLIMIT\s+\d+\s+BY\b|\bLIMIT\s+\d+\s+OFFSET\b|\bLIMIT\s+\d+\s*,|\bFORMAT\b|\bSETTINGS\b|^\s*EXPLAIN\b|^\s*DESC(RIBE)?\b|^\s*SHOW\s+CREATE\b|^\s*EXISTS\b|^\s*CHECK\b)`)

// EnforceLimit ensures the SQL has a LIMIT clause within bounds.
// If limit is 0, enforcement is disabled (pass-through).
// If the SQL contains LIMIT BY, FORMAT, SETTINGS, or a metadata statement
// (EXPLAIN/DESCRIBE/SHOW CREATE/EXISTS/CHECK), it bails out (pass-through).
func EnforceLimit(sql string, limit, maxLimit int) string {
	return querysql.EnforceLimit(sql, limit, maxLimit, limitBailRe.MatchString)
}

// QueryRequest represents a ClickHouse query request.
type QueryRequest struct {
	RawSQL     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}

// TableInfo represents a row from system.tables.
type TableInfo struct {
	Database   string  `json:"database"`
	Name       string  `json:"name"`
	Engine     string  `json:"engine"`
	TotalRows  *uint64 `json:"totalRows,omitempty"`
	TotalBytes *uint64 `json:"totalBytes,omitempty"`
}

// ColumnInfo represents a row from system.columns.
type ColumnInfo struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	DefaultType       string `json:"defaultType,omitempty"`
	DefaultExpression string `json:"defaultExpression,omitempty"`
	Comment           string `json:"comment,omitempty"`
}

// ParseTableInfoRows converts a QueryResponse from the system.tables query into typed TableInfo.
func ParseTableInfoRows(resp *querysql.QueryResponse) []TableInfo {
	tables := make([]TableInfo, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		if len(row) < 5 {
			continue
		}
		t := TableInfo{
			Database: fmt.Sprint(row[0]),
			Name:     fmt.Sprint(row[1]),
			Engine:   fmt.Sprint(row[2]),
		}
		if row[3] != nil {
			if v, ok := toUint64(row[3]); ok {
				t.TotalRows = &v
			}
		}
		if row[4] != nil {
			if v, ok := toUint64(row[4]); ok {
				t.TotalBytes = &v
			}
		}
		tables = append(tables, t)
	}
	return tables
}

// ParseColumnInfoRows converts a QueryResponse from the system.columns query into typed ColumnInfo.
func ParseColumnInfoRows(resp *querysql.QueryResponse) []ColumnInfo {
	cols := make([]ColumnInfo, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		if len(row) < 5 {
			continue
		}
		cols = append(cols, ColumnInfo{
			Name:              fmt.Sprint(row[0]),
			Type:              fmt.Sprint(row[1]),
			DefaultType:       fmt.Sprint(row[2]),
			DefaultExpression: fmt.Sprint(row[3]),
			Comment:           fmt.Sprint(row[4]),
		})
	}
	return cols
}

func toUint64(v any) (uint64, bool) {
	switch val := v.(type) {
	case float64:
		if val < 0 {
			return 0, false
		}
		return uint64(val), true
	default:
		return 0, false
	}
}
