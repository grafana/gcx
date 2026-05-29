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
	Name   string  `json:"name,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Field describes a field in a Grafana data frame.
type Field struct {
	Name   string            `json:"name,omitempty"`
	Type   string            `json:"type,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Data contains column-oriented Grafana data frame values.
type Data struct {
	Values [][]any `json:"values,omitempty"`
}
