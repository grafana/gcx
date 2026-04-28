package influxdb

import "time"

// Mode represents the InfluxDB query language mode.
type Mode string

const (
	ModeInfluxQL Mode = "InfluxQL"
	ModeFlux     Mode = "Flux"
	ModeSQL      Mode = "SQL"
)

// QueryRequest represents an InfluxDB query request.
type QueryRequest struct {
	Query string
	Start time.Time
	End   time.Time
	Step  time.Duration
	Mode  Mode
}

// IsRange returns true if this is a range query.
func (r QueryRequest) IsRange() bool {
	return !r.Start.IsZero() && !r.End.IsZero()
}

// QueryResponse represents the response from an InfluxDB query.
type QueryResponse struct {
	Columns     []string     `json:"columns"`
	Rows        [][]any      `json:"rows"`
	TimeColumns map[int]bool `json:"-"` // indices of millisecond-epoch time columns, not serialized
}

// MeasurementsResponse represents the response from a SHOW MEASUREMENTS query.
type MeasurementsResponse struct {
	Measurements []string `json:"measurements"`
}

// FieldKeysResponse represents the response from a SHOW FIELD KEYS query.
type FieldKeysResponse struct {
	Fields []FieldKey `json:"fields"`
}

// FieldKey represents a single field key with its type.
type FieldKey struct {
	FieldKey  string `json:"fieldKey"`
	FieldType string `json:"fieldType"`
}

// GrafanaQueryResponse represents the response from Grafana's datasource query API.
type GrafanaQueryResponse struct {
	Results map[string]GrafanaResult `json:"results"`
}

// GrafanaResult represents a single result from a Grafana query.
type GrafanaResult struct {
	Frames      []DataFrame `json:"frames,omitempty"`
	Error       string      `json:"error,omitempty"`
	ErrorSource string      `json:"errorSource,omitempty"`
	Status      int         `json:"status,omitempty"`
}

// DataFrame represents a Grafana data frame.
type DataFrame struct {
	Schema DataFrameSchema `json:"schema"`
	Data   DataFrameData   `json:"data"`
}

// DataFrameSchema describes the structure of a data frame.
type DataFrameSchema struct {
	Name   string  `json:"name,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Field describes a field in a data frame.
type Field struct {
	Name   string            `json:"name,omitempty"`
	Type   string            `json:"type,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// DataFrameData contains the actual data values.
type DataFrameData struct {
	Values [][]any `json:"values,omitempty"`
}
