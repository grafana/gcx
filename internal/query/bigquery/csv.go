package bigquery

import (
	"strings"

	"github.com/grafana/gcx/internal/query/dataframe"
)

func (r StringList) CSVRecords() ([]string, [][]string, error) {
	rows := make([][]string, 0, len(r.Items))
	for _, item := range r.Items {
		rows = append(rows, []string{item})
	}
	return []string{strings.ToLower(r.Header)}, rows, nil
}

func TableInfoCSVRecords(items []TableInfo) ([]string, [][]string, error) {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{item.Name, item.Type})
	}
	return dataframe.CSVRows([]string{"name", "type"}, rows)
}

func ColumnInfoCSVRecords(items []ColumnInfo) ([]string, [][]string, error) {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{item.Name, item.Type, item.Nullable})
	}
	return dataframe.CSVRows([]string{"name", "type", "nullable"}, rows)
}
