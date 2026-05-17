// Package dsabstraction provides a client for the dsabstraction.grafana.app
// SQL query API. The endpoint accepts a SQL string and returns a single
// Grafana data frame. See:
//
//	POST /apis/dsabstraction.grafana.app/v1alpha1/namespaces/{namespace}/query
package dsabstraction

import "encoding/json"

// SQLRequest is the input for a dsabstraction SQL query.
type SQLRequest struct {
	SQL  string
	From string
	To   string
	// Pushdown controls server-side pushdown of filters/aggregations to the
	// underlying datasource. nil leaves the choice to the server default.
	Pushdown *bool
	// Cookie is the literal Cookie header value to attach to the request
	// (e.g. "grafana_session=abc123"). Empty means no Cookie header is sent.
	// Used in local dev where the apiserver forwards the inbound auth to
	// Grafana for schema fetches and plain basic auth isn't accepted.
	Cookie string
}

// requestBody is the JSON body sent to the dsabstraction query endpoint.
type requestBody struct {
	Query    string `json:"query"`
	From     string `json:"from"`
	To       string `json:"to"`
	Pushdown *bool  `json:"pushdown,omitempty"`
}

// SQLResponse is the response from a dsabstraction SQL query.
//
// The wire shape is a single Grafana data frame with column-major Values:
//
//	{
//	  "schema": {"name": "...", "meta": {...}, "fields": [{name,type,...}]},
//	  "data":   {"values": [[...col0...], [...col1...], ...]}
//	}
type SQLResponse struct {
	Schema FrameSchema `json:"schema"`
	Data   FrameData   `json:"data"`
}

// FrameSchema describes the structure of a returned frame.
type FrameSchema struct {
	Name   string     `json:"name,omitempty"`
	Meta   *FrameMeta `json:"meta,omitempty"`
	Fields []Field    `json:"fields,omitempty"`
}

// FrameMeta carries free-form metadata attached to a frame. Custom is left as
// raw JSON so server-side additions don't force client changes.
type FrameMeta struct {
	TypeVersion []int           `json:"typeVersion,omitempty"`
	Custom      json.RawMessage `json:"custom,omitempty"`
}

// Field describes a single column in a returned frame.
type Field struct {
	Name     string            `json:"name,omitempty"`
	Type     string            `json:"type,omitempty"`
	TypeInfo *FieldTypeInfo    `json:"typeInfo,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// FieldTypeInfo describes the wire-level type of a field's values.
type FieldTypeInfo struct {
	Frame    string `json:"frame,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
}

// FrameData holds the column-major values of a frame. Values[col][row] is the
// cell at the given column and row.
type FrameData struct {
	Values [][]any `json:"values,omitempty"`
}

// PushdownPlanEntry is one entry in the optional pushdown plan emitted under
// schema.meta.custom.pushdownPlan. It is decoded lazily by ParsePushdownPlan.
type PushdownPlanEntry struct {
	Handler string `json:"handler,omitempty"`
	Node    string `json:"node,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Pushed  bool   `json:"pushed,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// ParsePushdownPlan extracts the pushdownPlan entries from the frame's
// schema.meta.custom blob. Returns nil, nil when no plan is present.
func (r *SQLResponse) ParsePushdownPlan() ([]PushdownPlanEntry, error) {
	if r == nil || r.Schema.Meta == nil || len(r.Schema.Meta.Custom) == 0 {
		return nil, nil
	}
	var wrapper struct {
		PushdownPlan []PushdownPlanEntry `json:"pushdownPlan"`
	}
	if err := json.Unmarshal(r.Schema.Meta.Custom, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.PushdownPlan, nil
}

// RowCount returns the number of rows in the frame, derived from the longest
// column. Empty frames return 0.
func (r *SQLResponse) RowCount() int {
	if r == nil {
		return 0
	}
	longest := 0
	for _, col := range r.Data.Values {
		if len(col) > longest {
			longest = len(col)
		}
	}
	return longest
}
