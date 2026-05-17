// Package schemads provides a client for the schemads schema-discovery
// protocol exposed by Grafana datasource plugins under
// /api/datasources/uid/{uid}/resources/abstractionSchema/...
//
// Plugins that implement this protocol publish their tables, columns,
// table hints, and aggregation capabilities so that callers (UIs, query
// engines, this CLI) can introspect what they can be asked.
package schemads

// FullSchemaResponse is the response body of the fullSchema endpoint.
type FullSchemaResponse struct {
	FullSchema *Schema `json:"fullSchema,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// Schema is the complete schema reported by a datasource.
type Schema struct {
	Tables       []Table                 `json:"tables,omitempty"`
	Functions    []string                `json:"functions,omitempty"`
	Capabilities *DatasourceCapabilities `json:"capabilities,omitempty"`
}

// Table describes a single virtual table the datasource exposes.
type Table struct {
	Name            string           `json:"name"`
	TableParameters []TableParameter `json:"tableParameters,omitempty"`
	Columns         []Column         `json:"columns,omitempty"`
	TableHints      []TableHint      `json:"tableHints,omitempty"`
	Metadata        Metadata         `json:"metadata,omitzero"`
}

// Column is a single column in a Table.
type Column struct {
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Operators []Operator `json:"operators,omitempty"`
	// Description is the legacy doc field; producers may still populate
	// it for one release of the protocol. Prefer Metadata.Description.
	Description string   `json:"description,omitempty"`
	Metadata    Metadata `json:"metadata,omitzero"`
}

// Metadata carries optional descriptive information about a Table or Column
// (e.g. Prometheus HELP/TYPE, SQL COMMENT, OpenAPI field docs). Description
// and Unit are well-known typed slots; anything datasource-specific belongs
// in Custom (lowercase, namespaced keys like "prom.type").
type Metadata struct {
	Description string         `json:"description,omitempty"`
	Unit        string         `json:"unit,omitempty"`
	Custom      map[string]any `json:"custom,omitempty"`
}

// IsZero reports whether the metadata carries no information.
func (m Metadata) IsZero() bool {
	return m.Description == "" && m.Unit == "" && len(m.Custom) == 0
}

// Operator is a filter operator a column supports.
type Operator string

// ColumnsResponse is the response body of the columns endpoint, keyed by
// table name. The endpoint returns dynamic per-table columns (e.g. for a
// Prometheus metric, label dimensions are included alongside timestamp/value),
// plus optional table-level metadata that producers populate lazily here
// when assembling it requires per-table upstream calls.
type ColumnsResponse struct {
	Columns       map[string][]Column `json:"columns"`
	TableMetadata map[string]Metadata `json:"tableMetadata,omitempty"`
	Errors        map[string]string   `json:"errors,omitempty"`
}

// TableParameter is a parameter accepted by a parameterised table.
type TableParameter struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Root      bool     `json:"root,omitempty"`
	Required  bool     `json:"required,omitempty"`
}

// TableHint is a free-form hint a caller can attach to a FROM-clause
// table reference (e.g. `rate('5m')`).
type TableHint struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	HasValue    bool   `json:"hasValue,omitempty"`
}

// DatasourceCapabilities is the datasource-level capability advertisement.
type DatasourceCapabilities struct {
	AggregateFunctions []string `json:"aggregateFunctions,omitempty"`
	OrderBy            bool     `json:"orderBy,omitempty"`
	Limit              bool     `json:"limit,omitempty"`
}
