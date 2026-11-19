package sql

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TableConstraints is the stable response returned by describe-constraints.
// Namespace is the MySQL database or PostgreSQL schema.
type TableConstraints struct {
	Table       TableIdentity     `json:"table"`
	Constraints []TableConstraint `json:"constraints"`
}

// TableIdentity identifies a table in a datasource namespace.
type TableIdentity struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// TableConstraint describes one table constraint. Referenced is populated
// only for foreign-key constraints. Columns and Referenced.Columns retain the
// ordinal order reported by the database, including for composite keys.
type TableConstraint struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Columns    []string        `json:"columns"`
	Referenced *TableReference `json:"referencedTable,omitempty"`
	OnDelete   string          `json:"onDelete,omitempty"`
	OnUpdate   string          `json:"onUpdate,omitempty"`
}

// TableReference identifies the target of a foreign-key constraint.
type TableReference struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Columns   []string `json:"columns"`
}

type constraintColumn struct {
	ordinal int
	seen    int
	name    string
}

type constraintAccumulator struct {
	constraint        TableConstraint
	columns           []constraintColumn
	referencedColumns []constraintColumn
}

// ParseTableConstraints groups normalized catalog rows by constraint name and
// type, retaining the declared order of local and referenced key columns.
func ParseTableConstraints(namespace, table string, resp *QueryResponse) (*TableConstraints, error) {
	if resp == nil {
		return nil, errors.New("constraints response is nil")
	}
	desc := &TableConstraints{Table: TableIdentity{Namespace: namespace, Name: table}, Constraints: make([]TableConstraint, 0)}
	// Grafana's MySQL plugin returns an empty frame as {columns:null,rows:null}
	// when information_schema has no matching constraints. Treat that as a
	// valid table with no declared constraints; there are no rows whose aliases
	// need validation.
	if len(resp.Rows) == 0 {
		return desc, nil
	}

	// Validate the aliases once, before touching any row. A malformed or
	// changed provider query should fail clearly instead of silently returning
	// incomplete relationship metadata.
	for _, alias := range []string{"constraint_name", "constraint_type", "ordinal_position", "column_name", "referenced_namespace", "referenced_table", "referenced_column", "on_delete", "on_update"} {
		if findColumn(resp, alias) < 0 {
			return nil, fmt.Errorf("constraints response is missing column %q", alias)
		}
	}

	byIdentity := make(map[[2]string]*constraintAccumulator)
	order := make([]*constraintAccumulator, 0)
	for rowIndex, row := range resp.Rows {
		name := valueAsString(rowValue(resp, row, "constraint_name"))
		typ := valueAsString(rowValue(resp, row, "constraint_type"))
		if name == "" || typ == "" {
			return nil, errors.New("constraint name and type must not be empty")
		}

		key := [2]string{name, typ}
		acc := byIdentity[key]
		if acc == nil {
			acc = &constraintAccumulator{
				constraint: TableConstraint{Name: name, Type: typ},
			}
			byIdentity[key] = acc
			order = append(order, acc)
		}

		ordinal, err := valueAsInt(rowValue(resp, row, "ordinal_position"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q ordinal position: %w", name, err)
		}
		columnName := valueAsString(rowValue(resp, row, "column_name"))
		if columnName != "" {
			acc.columns = append(acc.columns, constraintColumn{ordinal: ordinal, seen: rowIndex, name: columnName})
		}

		refNamespace := valueAsString(rowValue(resp, row, "referenced_namespace"))
		refTable := valueAsString(rowValue(resp, row, "referenced_table"))
		refColumn := valueAsString(rowValue(resp, row, "referenced_column"))
		if refNamespace != "" || refTable != "" || refColumn != "" {
			if acc.constraint.Referenced == nil {
				acc.constraint.Referenced = &TableReference{Namespace: refNamespace, Name: refTable}
			} else if acc.constraint.Referenced.Namespace != refNamespace || acc.constraint.Referenced.Name != refTable {
				return nil, fmt.Errorf("constraint %q has inconsistent referenced table", name)
			}
			if refColumn != "" {
				acc.referencedColumns = append(acc.referencedColumns, constraintColumn{ordinal: ordinal, seen: rowIndex, name: refColumn})
			}
		}

		deleteRule := valueAsString(rowValue(resp, row, "on_delete"))
		updateRule := valueAsString(rowValue(resp, row, "on_update"))
		if deleteRule != "" {
			if acc.constraint.OnDelete != "" && acc.constraint.OnDelete != deleteRule {
				return nil, fmt.Errorf("constraint %q has inconsistent delete actions", name)
			}
			acc.constraint.OnDelete = deleteRule
		}
		if updateRule != "" {
			if acc.constraint.OnUpdate != "" && acc.constraint.OnUpdate != updateRule {
				return nil, fmt.Errorf("constraint %q has inconsistent update actions", name)
			}
			acc.constraint.OnUpdate = updateRule
		}
	}

	for _, acc := range order {
		acc.constraint.Columns = orderedConstraintColumns(acc.columns)
		if acc.constraint.Referenced != nil {
			acc.constraint.Referenced.Columns = orderedConstraintColumns(acc.referencedColumns)
		}
		desc.Constraints = append(desc.Constraints, acc.constraint)
	}

	return desc, nil
}

func orderedConstraintColumns(columns []constraintColumn) []string {
	sort.SliceStable(columns, func(i, j int) bool {
		if columns[i].ordinal == columns[j].ordinal {
			return columns[i].seen < columns[j].seen
		}
		if columns[i].ordinal == 0 {
			return false
		}
		if columns[j].ordinal == 0 {
			return true
		}
		return columns[i].ordinal < columns[j].ordinal
	})
	names := make([]string, 0, len(columns))
	for _, col := range columns {
		names = append(names, col.name)
	}
	return names
}

func findColumn(resp *QueryResponse, name string) int {
	for i, col := range resp.Columns {
		if strings.EqualFold(col.Name, name) {
			return i
		}
	}
	return -1
}

func rowValue(resp *QueryResponse, row []any, name string) any {
	idx := findColumn(resp, name)
	if idx < 0 || idx >= len(row) {
		return nil
	}
	return row[idx]
}

func valueAsString(value any) string {
	if value == nil {
		return ""
	}
	if v, ok := value.([]byte); ok {
		return string(v)
	}
	return fmt.Sprint(value)
}

func valueAsInt(value any) (int, error) {
	if value == nil {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("ordinal position must not be negative")
	}
	return n, nil
}
