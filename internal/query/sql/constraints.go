package sql

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TableDescription is the structured response returned by describe-table
// when constraint metadata is requested. Namespace is the database for MySQL
// and the schema for PostgreSQL. The shape intentionally differs from the
// legacy QueryResponse so the opt-in response can carry table identity and
// relationships without changing the default output contract.
type TableDescription struct {
	Table       TableIdentity     `json:"table"`
	Columns     []TableColumn     `json:"columns"`
	Constraints []TableConstraint `json:"constraints"`
}

// TableIdentity identifies a table in a datasource namespace.
type TableIdentity struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// TableColumn describes a column in a TableDescription.
type TableColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
	Default  any    `json:"default"`
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

// ParseTableDescription combines the column response and a normalized
// constraint response into the stable describe-table envelope. The
// constraint query used by each SQL dialect must expose these column aliases:
// constraint_name, constraint_type, ordinal_position, column_name,
// referenced_namespace, referenced_table, referenced_column, on_delete, and
// on_update. Parsing by aliases rather than positional indexes keeps the
// response resilient to harmless SELECT-list changes.
func ParseTableDescription(namespace, table string, columnsResp, constraintsResp *QueryResponse) (*TableDescription, error) {
	if columnsResp == nil {
		return nil, fmt.Errorf("columns response is nil")
	}
	if constraintsResp == nil {
		return nil, fmt.Errorf("constraints response is nil")
	}

	desc := &TableDescription{
		Table: TableIdentity{Namespace: namespace, Name: table},
	}
	for _, row := range columnsResp.Rows {
		name, err := valueAsString(rowValue(columnsResp, row, "name"))
		if err != nil {
			return nil, fmt.Errorf("parse column name: %w", err)
		}
		typ, err := valueAsString(rowValue(columnsResp, row, "type"))
		if err != nil {
			return nil, fmt.Errorf("parse column type: %w", err)
		}
		nullable, err := valueAsString(rowValue(columnsResp, row, "nullable"))
		if err != nil {
			return nil, fmt.Errorf("parse column nullable: %w", err)
		}
		if name == "" {
			return nil, fmt.Errorf("column name is empty")
		}
		desc.Columns = append(desc.Columns, TableColumn{
			Name:     name,
			Type:     typ,
			Nullable: nullable,
			Default:  rowValue(columnsResp, row, "default"),
		})
	}

	return parseConstraints(desc, constraintsResp)
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

func parseConstraints(desc *TableDescription, resp *QueryResponse) (*TableDescription, error) {
	// Validate the aliases once, before touching any row. A malformed or
	// changed provider query should fail clearly instead of silently returning
	// incomplete relationship metadata.
	for _, alias := range []string{"constraint_name", "constraint_type", "ordinal_position", "column_name", "referenced_namespace", "referenced_table", "referenced_column", "on_delete", "on_update"} {
		if findColumn(resp, alias) < 0 {
			return nil, fmt.Errorf("constraints response is missing column %q", alias)
		}
	}

	byName := make(map[string]*constraintAccumulator)
	order := make([]string, 0)
	for rowIndex, row := range resp.Rows {
		name, err := valueAsString(rowValue(resp, row, "constraint_name"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint name: %w", err)
		}
		typ, err := valueAsString(rowValue(resp, row, "constraint_type"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint type: %w", err)
		}
		if name == "" || typ == "" {
			return nil, fmt.Errorf("constraint name and type must not be empty")
		}

		acc := byName[name]
		if acc == nil {
			acc = &constraintAccumulator{
				constraint: TableConstraint{Name: name, Type: typ},
			}
			byName[name] = acc
			order = append(order, name)
		} else if acc.constraint.Type != typ {
			return nil, fmt.Errorf("constraint %q has inconsistent types %q and %q", name, acc.constraint.Type, typ)
		}

		ordinal, err := valueAsInt(rowValue(resp, row, "ordinal_position"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q ordinal position: %w", name, err)
		}
		columnName, err := valueAsString(rowValue(resp, row, "column_name"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q column: %w", name, err)
		}
		if columnName != "" {
			acc.columns = append(acc.columns, constraintColumn{ordinal: ordinal, seen: rowIndex, name: columnName})
		}

		refNamespace, err := valueAsString(rowValue(resp, row, "referenced_namespace"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q referenced namespace: %w", name, err)
		}
		refTable, err := valueAsString(rowValue(resp, row, "referenced_table"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q referenced table: %w", name, err)
		}
		refColumn, err := valueAsString(rowValue(resp, row, "referenced_column"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q referenced column: %w", name, err)
		}
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

		deleteRule, err := valueAsString(rowValue(resp, row, "on_delete"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q delete action: %w", name, err)
		}
		updateRule, err := valueAsString(rowValue(resp, row, "on_update"))
		if err != nil {
			return nil, fmt.Errorf("parse constraint %q update action: %w", name, err)
		}
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

	for _, name := range order {
		acc := byName[name]
		sort.SliceStable(acc.columns, func(i, j int) bool {
			if acc.columns[i].ordinal == acc.columns[j].ordinal {
				return acc.columns[i].seen < acc.columns[j].seen
			}
			// A nil ordinal is represented as zero. Keep it after real
			// ordinals while preserving source order among nils.
			if acc.columns[i].ordinal == 0 {
				return false
			}
			if acc.columns[j].ordinal == 0 {
				return true
			}
			return acc.columns[i].ordinal < acc.columns[j].ordinal
		})
		acc.constraint.Columns = make([]string, 0, len(acc.columns))
		for _, col := range acc.columns {
			acc.constraint.Columns = append(acc.constraint.Columns, col.name)
		}
		if acc.constraint.Referenced != nil {
			sort.SliceStable(acc.referencedColumns, func(i, j int) bool {
				if acc.referencedColumns[i].ordinal == acc.referencedColumns[j].ordinal {
					return acc.referencedColumns[i].seen < acc.referencedColumns[j].seen
				}
				if acc.referencedColumns[i].ordinal == 0 {
					return false
				}
				if acc.referencedColumns[j].ordinal == 0 {
					return true
				}
				return acc.referencedColumns[i].ordinal < acc.referencedColumns[j].ordinal
			})
			acc.constraint.Referenced.Columns = make([]string, 0, len(acc.referencedColumns))
			for _, col := range acc.referencedColumns {
				acc.constraint.Referenced.Columns = append(acc.constraint.Referenced.Columns, col.name)
			}
		}
		desc.Constraints = append(desc.Constraints, acc.constraint)
	}

	return desc, nil
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

func valueAsString(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case fmt.Stringer:
		return v.String(), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func valueAsInt(value any) (int, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case float32:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
		if err != nil {
			return 0, err
		}
		return n, nil
	}
}
