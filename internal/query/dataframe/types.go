package dataframe

// Response is the top-level wire format returned by Grafana's datasource
// query APIs (/apis/query.grafana.app/.../query and /api/ds/query).
type Response struct {
	Results map[string]Result `json:"results"`
}

// Result represents a single refId result from a Grafana datasource query.
type Result struct {
	Frames      []Frame `json:"frames,omitempty"`
	Error       string  `json:"error,omitempty"`
	ErrorSource string  `json:"errorSource,omitempty"`
	Status      int     `json:"status,omitempty"`
}

// Frame represents a Grafana data frame.
type Frame struct {
	Schema Schema `json:"schema"`
	Data   Data   `json:"data"`
}

// Schema describes a Grafana data frame schema.
type Schema struct {
	RefId  string  `json:"refId,omitempty"`
	Name   string  `json:"name,omitempty"`
	Meta   *Meta   `json:"meta,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Meta contains metadata about a data frame, such as query execution stats
// and notices surfaced by the datasource plugin.
type Meta struct {
	Type                string   `json:"type,omitempty"`
	Stats               []Stat   `json:"stats,omitempty"`
	Notices             []Notice `json:"notices,omitempty"`
	ExecutedQueryString string   `json:"executedQueryString,omitempty"`
}

// Stat represents a single statistic from query execution.
type Stat struct {
	DisplayName string  `json:"displayName"`
	Unit        string  `json:"unit,omitempty"`
	Value       float64 `json:"value"`
}

// Notice represents a notice or warning attached to a data frame.
type Notice struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

// Field describes a field in a Grafana data frame.
type Field struct {
	Name   string            `json:"name,omitempty"`
	Type   string            `json:"type,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Config *FieldConfig      `json:"config,omitempty"`
}

// FieldConfig holds display/formatting hints for a data frame field.
type FieldConfig struct {
	DisplayNameFromDS string `json:"displayNameFromDS,omitempty"`
	Unit              string `json:"unit,omitempty"`
}

// Data contains column-oriented Grafana data frame values.
type Data struct {
	Values [][]any `json:"values,omitempty"`
	Nanos  [][]int `json:"nanos,omitempty"`
}
