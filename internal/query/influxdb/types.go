package influxdb

import (
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
)

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

// TagKeysResponse represents the response from a SHOW TAG KEYS query.
type TagKeysResponse struct {
	TagKeys []string `json:"tagKeys"`
}

// TagValuesResponse represents the response from a SHOW TAG VALUES query.
type TagValuesResponse struct {
	Values []TagValue `json:"values"`
}

// TagValue represents a single tag key-value pair.
type TagValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GrafanaQueryResponse is the top-level Grafana datasource query response.
type GrafanaQueryResponse = dataframe.Response

// GrafanaResult represents a single Grafana datasource query result.
type GrafanaResult = dataframe.Result

// DataFrame represents a Grafana data frame.
type DataFrame = dataframe.Frame

// DataFrameSchema describes the structure of a Grafana data frame.
type DataFrameSchema = dataframe.Schema

// Field describes a field in a Grafana data frame.
type Field = dataframe.Field

// DataFrameData contains column-oriented Grafana data frame values.
type DataFrameData = dataframe.Data
