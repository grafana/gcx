package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/dataframe"
)

// stateHistoryPath is the Grafana alerting state-history endpoint. It returns a
// single Grafana data frame (schema + column-oriented data) describing the
// state transitions recorded by the configured history backend (Loki or
// annotations).
const stateHistoryPath = "/api/v1/rules/history"

// State-history frame column names, as emitted by Grafana's history builder
// (grafana/pkg/services/ngalert/state/historian): a merged history of one
// timestamp, one JSON "line", and one JSON "labels" object per transition.
const (
	dfColTime   = "time"
	dfColLine   = "line"
	dfColLabels = "labels"
)

// StateHistoryOptions configures a state-history query.
type StateHistoryOptions struct {
	// RuleUID filters to a single rule. The annotations history backend
	// requires it; the Loki backend treats it as optional.
	RuleUID string
	// From and To bound the query time range. A zero value omits that bound.
	From time.Time
	To   time.Time
	// Limit caps the number of returned records. A value <= 0 omits the bound
	// and lets the backend apply its own default.
	Limit int
	// Labels are instance-label equality filters, sent to the API as
	// labels_<key>=<value> query parameters.
	Labels map[string]string
}

// StateTransition is one recorded alert state change, flattened from the
// state-history data frame for display and machine consumption.
type StateTransition struct {
	Time         time.Time         `json:"time"`
	RuleUID      string            `json:"ruleUid,omitempty"`
	RuleTitle    string            `json:"ruleTitle,omitempty"`
	Previous     string            `json:"previous"`
	Current      string            `json:"current"`
	Error        string            `json:"error,omitempty"`
	Values       map[string]any    `json:"values,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
	DashboardUID string            `json:"dashboardUid,omitempty"`
	PanelID      int64             `json:"panelId,omitempty"`
}

// lokiLine mirrors the JSON object stored in the frame's "line" column
// (grafana/pkg/services/ngalert/state/historian.LokiEntry). Only the fields
// gcx surfaces are decoded.
type lokiLine struct {
	Previous     string            `json:"previous"`
	Current      string            `json:"current"`
	Error        string            `json:"error"`
	Values       map[string]any    `json:"values"`
	DashboardUID string            `json:"dashboardUID"`
	PanelID      int64             `json:"panelID"`
	Fingerprint  string            `json:"fingerprint"`
	RuleTitle    string            `json:"ruleTitle"`
	RuleUID      string            `json:"ruleUID"`
	Labels       map[string]string `json:"labels"`
}

// QueryStateHistory queries alert state history and returns the recorded
// transitions ordered newest-first.
func (c *Client) QueryStateHistory(ctx context.Context, opts StateHistoryOptions) ([]StateTransition, error) {
	params := url.Values{}
	if opts.RuleUID != "" {
		params.Set("ruleUID", opts.RuleUID)
	}
	if !opts.From.IsZero() {
		params.Set("from", strconv.FormatInt(opts.From.Unix(), 10))
	}
	if !opts.To.IsZero() {
		params.Set("to", strconv.FormatInt(opts.To.Unix(), 10))
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	for k, v := range opts.Labels {
		params.Set("labels_"+k, v)
	}

	path := stateHistoryPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	frame, err := c.getStateHistoryFrame(ctx, path)
	if err != nil {
		return nil, err
	}
	return transitionsFromFrame(frame)
}

func (c *Client) getStateHistoryFrame(ctx context.Context, path string) (*dataframe.Frame, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	// The endpoint returns a bare Grafana data frame ({schema, data}), not the
	// {results: {...}} envelope of the datasource query API.
	var frame dataframe.Frame
	if err := json.NewDecoder(resp.Body).Decode(&frame); err != nil {
		return nil, fmt.Errorf("failed to decode state history response: %w", err)
	}
	return &frame, nil
}

// transitionsFromFrame flattens the "states" data frame into StateTransition
// records ordered newest-first. It resolves columns by schema field name and
// falls back to the documented positional order (time, line, labels).
func transitionsFromFrame(frame *dataframe.Frame) ([]StateTransition, error) {
	cols := make(map[string]int, len(frame.Schema.Fields))
	for i, f := range frame.Schema.Fields {
		cols[f.Name] = i
	}

	timeCol := columnValues(frame, cols, dfColTime, 0)
	lineCol := columnValues(frame, cols, dfColLine, 1)
	labelCol := columnValues(frame, cols, dfColLabels, 2)

	transitions := make([]StateTransition, 0, len(timeCol))
	for i := range timeCol {
		st := StateTransition{Time: frameTime(timeCol[i])}

		if i < len(lineCol) {
			line, err := decodeLine(lineCol[i])
			if err != nil {
				return nil, err
			}
			st.RuleUID = line.RuleUID
			st.RuleTitle = line.RuleTitle
			st.Previous = line.Previous
			st.Current = line.Current
			st.Error = line.Error
			st.Values = line.Values
			st.Fingerprint = line.Fingerprint
			st.DashboardUID = line.DashboardUID
			st.PanelID = line.PanelID
			st.Labels = line.Labels
		}

		// Fall back to the stream-labels column when the line carried none.
		if len(st.Labels) == 0 && i < len(labelCol) {
			if lbls, err := decodeLabels(labelCol[i]); err == nil {
				st.Labels = lbls
			}
		}

		transitions = append(transitions, st)
	}

	sort.SliceStable(transitions, func(i, j int) bool {
		return transitions[i].Time.After(transitions[j].Time)
	})
	return transitions, nil
}

// columnValues returns the values for the named schema field, falling back to
// the given positional index when the schema does not name it.
func columnValues(frame *dataframe.Frame, cols map[string]int, name string, fallback int) []any {
	idx, ok := cols[name]
	if !ok {
		idx = fallback
	}
	if idx < 0 || idx >= len(frame.Data.Values) {
		return nil
	}
	return frame.Data.Values[idx]
}

// decodeLine re-encodes the decoded "line" value and unmarshals it into the
// typed LokiEntry subset gcx surfaces.
func decodeLine(v any) (lokiLine, error) {
	var line lokiLine
	// The frame value may arrive already decoded (map[string]any) or as raw
	// JSON; a round-trip normalizes both into the typed struct.
	raw, err := json.Marshal(v)
	if err != nil {
		return line, fmt.Errorf("failed to re-encode state history line: %w", err)
	}
	if err := json.Unmarshal(raw, &line); err != nil {
		return line, fmt.Errorf("failed to decode state history line: %w", err)
	}
	return line, nil
}

func decodeLabels(v any) (map[string]string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var labels map[string]string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// frameTime converts a data-frame time value (epoch milliseconds) to a UTC
// time. It returns the zero time for unrecognized encodings.
func frameTime(v any) time.Time {
	switch t := v.(type) {
	case float64:
		return time.UnixMilli(int64(t)).UTC()
	case json.Number:
		if ms, err := t.Int64(); err == nil {
			return time.UnixMilli(ms).UTC()
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
