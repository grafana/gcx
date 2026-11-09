package clickhouse

import "github.com/grafana/gcx/internal/query/dataframe"

func TableInfoCSVRecords(items []TableInfo) ([]string, [][]string, error) {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{item.Database, item.Name, item.Engine, item.TotalRows, item.TotalBytes})
	}
	return dataframe.CSVRows([]string{"database", "name", "engine", "total_rows", "total_bytes"}, rows)
}

func ColumnInfoCSVRecords(items []ColumnInfo) ([]string, [][]string, error) {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{item.Name, item.Type, item.DefaultType, item.DefaultExpression, item.Comment})
	}
	return dataframe.CSVRows([]string{"name", "type", "default_type", "default_expression", "comment"}, rows)
}
