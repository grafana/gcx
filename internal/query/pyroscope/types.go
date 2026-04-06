package pyroscope

import (
	"encoding/json"
	"sort"
	"time"
)

// QueryRequest represents a Pyroscope profile query request.
type QueryRequest struct {
	LabelSelector string
	ProfileTypeID string
	Start         time.Time
	End           time.Time
	MaxNodes      int64
}

// IsRange returns true if this is a range query with explicit time bounds.
func (r QueryRequest) IsRange() bool {
	return !r.Start.IsZero() && !r.End.IsZero()
}

// QueryResponse represents the response from a Pyroscope profile query.
type QueryResponse struct {
	Flamegraph *Flamegraph `json:"flamegraph,omitempty"`
}

// Flamegraph represents a flame graph structure.
type Flamegraph struct {
	Names   []string `json:"names"`
	Levels  []Level  `json:"levels"`
	Total   int64    `json:"total,string"`
	MaxSelf int64    `json:"maxSelf,string"`
}

// Level represents a single level in the flame graph.
type Level struct {
	Values []string `json:"values"` // API returns strings that need to be parsed
}

// ProfileTypesRequest represents a request to list profile types.
type ProfileTypesRequest struct {
	Start time.Time
	End   time.Time
}

// ProfileTypesResponse represents the response from a profile types query.
type ProfileTypesResponse struct {
	ProfileTypes []ProfileType `json:"profileTypes"`
}

// ProfileType represents a profile type in Pyroscope.
type ProfileType struct {
	ID         string `json:"ID"`
	Name       string `json:"name"`
	SampleType string `json:"sampleType"`
	SampleUnit string `json:"sampleUnit"`
	PeriodType string `json:"periodType"`
	PeriodUnit string `json:"periodUnit"`
}

// LabelNamesRequest represents a request to list label names.
type LabelNamesRequest struct {
	Matchers []string
	Start    time.Time
	End      time.Time
}

// LabelNamesResponse represents the response from a label names query.
type LabelNamesResponse struct {
	Names []string `json:"names"`
}

// LabelValuesRequest represents a request to list label values.
type LabelValuesRequest struct {
	Name     string
	Matchers []string
	Start    time.Time
	End      time.Time
}

// LabelValuesResponse represents the response from a label values query.
type LabelValuesResponse struct {
	Names []string `json:"names"` // Pyroscope uses "names" for both labels and values
}

// FunctionSample represents a function in the flame graph with computed stats.
type FunctionSample struct {
	Name       string
	Self       int64
	Total      int64
	Percentage float64
}

// SelectSeriesRequest represents a request to query profile time-series data.
type SelectSeriesRequest struct {
	ProfileTypeID string
	LabelSelector string
	Start         time.Time
	End           time.Time
	GroupBy       []string
	Step          float64 // resolution step in seconds
	Aggregation   string  // "SUM" or "AVERAGE"
	Limit         int64   // top-N series by total value
}

// SelectSeriesResponse represents the response from a SelectSeries query.
type SelectSeriesResponse struct {
	Series []TimeSeries `json:"series"`
}

// TimeSeries represents a single time series with labels and data points.
type TimeSeries struct {
	Labels []LabelPair `json:"labels"`
	Points []TimePoint `json:"points"`
}

// LabelPair represents a key-value label pair.
type LabelPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// TimePoint represents a single data point in a time series.
// Pyroscope's connect-rpc JSON encoding sends timestamp as a string and value as an integer.
type TimePoint struct {
	Value       json.Number  `json:"value"`
	Timestamp   json.Number  `json:"timestamp"` // milliseconds since epoch, encoded as string
	Annotations []Annotation `json:"annotations,omitempty"`
}

// Annotation represents metadata attached to a time-series point.
type Annotation struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TimestampMs returns the timestamp as milliseconds since epoch.
func (p TimePoint) TimestampMs() int64 {
	v, _ := p.Timestamp.Int64()
	return v
}

// FloatValue returns the point value as float64.
func (p TimePoint) FloatValue() float64 {
	v, _ := p.Value.Float64()
	return v
}

// TopSeriesResponse represents an aggregated, ranked view of series data.
type TopSeriesResponse struct {
	ProfileType string           `json:"profileType"`
	GroupBy     []string         `json:"groupBy"`
	Series      []TopSeriesEntry `json:"series"`
}

// TopSeriesEntry represents a single ranked entry in a top-series response.
type TopSeriesEntry struct {
	Rank   int               `json:"rank"`
	Labels map[string]string `json:"labels"`
	Total  float64           `json:"total"`
}

// AggregateTopSeries converts a SelectSeriesResponse into a ranked TopSeriesResponse
// by summing all points per series and sorting by total descending.
func AggregateTopSeries(resp *SelectSeriesResponse, profileType string, groupBy []string, limit int) *TopSeriesResponse {
	type entry struct {
		labels map[string]string
		total  float64
	}

	entries := make([]entry, 0, len(resp.Series))
	for _, s := range resp.Series {
		var total float64
		for _, p := range s.Points {
			total += p.FloatValue()
		}
		lbls := make(map[string]string, len(s.Labels))
		for _, lp := range s.Labels {
			lbls[lp.Name] = lp.Value
		}
		entries = append(entries, entry{labels: lbls, total: total})
	}

	// Sort by total descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].total > entries[j].total
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	result := &TopSeriesResponse{
		ProfileType: profileType,
		GroupBy:     groupBy,
		Series:      make([]TopSeriesEntry, len(entries)),
	}
	for i, e := range entries {
		result.Series[i] = TopSeriesEntry{
			Rank:   i + 1,
			Labels: e.labels,
			Total:  e.total,
		}
	}
	return result
}
