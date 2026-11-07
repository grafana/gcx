package sql

import (
	"io"
	"strings"
)

// FormatConstraintsTable projects the same constraint envelope used by JSON and
// YAML into one table row per constraint.
func FormatConstraintsTable(w io.Writer, result *TableConstraints, wide bool) error {
	resp := &QueryResponse{Columns: []Column{
		{Name: "NAME"}, {Name: "TYPE"}, {Name: "COLUMNS"},
		{Name: "REFERENCED TABLE"}, {Name: "REFERENCED COLUMNS"},
		{Name: "ON DELETE"}, {Name: "ON UPDATE"},
	}}
	for _, constraint := range result.Constraints {
		var target, columns string
		if constraint.Referenced != nil {
			target = constraint.Referenced.Namespace + "." + constraint.Referenced.Name
			columns = strings.Join(constraint.Referenced.Columns, ", ")
		}
		resp.Rows = append(resp.Rows, []any{
			constraint.Name, constraint.Type, strings.Join(constraint.Columns, ", "),
			target, columns, constraint.OnDelete, constraint.OnUpdate,
		})
	}
	if wide {
		return FormatWideTable(w, resp)
	}
	return FormatTable(w, resp)
}
