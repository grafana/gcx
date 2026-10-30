package pyroscope

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// AnomaliesResult is sorted by score ascending: the most anomalous entry
// comes first.
type AnomaliesResult struct {
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Anomalies []Anomaly `json:"anomalies"`
}

type Anomaly struct {
	ProfileID string            `json:"profileId"`
	Timestamp time.Time         `json:"timestamp"`
	Score     float64           `json:"score"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// BuildAnomaliesResult only reads StacktraceAnomalies; a future anomaly
// source gets its own field here, not a shared one.
func BuildAnomaliesResult(resp *QueryAnomaliesResponse, from, to time.Time, topN int) *AnomaliesResult {
	entries := make([]Anomaly, len(resp.StacktraceAnomalies))
	for i, a := range resp.StacktraceAnomalies {
		entries[i] = Anomaly{
			ProfileID: a.ProfileID,
			Timestamp: time.UnixMilli(a.TimestampMs()).UTC(),
			Score:     a.Score,
			Labels:    labelPairsToMap(a.Labels),
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Score < entries[j].Score })
	if topN > 0 && len(entries) > topN {
		entries = entries[:topN]
	}

	return &AnomaliesResult{
		From:      from,
		To:        to,
		Anomalies: entries,
	}
}

// FormatAnomaliesTable hides label columns entirely when maxLabelCols is 0;
// otherwise it shows the N highest-cardinality label names.
func FormatAnomaliesTable(w io.Writer, result *AnomaliesResult, maxLabelCols int) error {
	labelMaps := make([]map[string]string, len(result.Anomalies))
	for i, a := range result.Anomalies {
		labelMaps[i] = a.Labels
	}
	cols := TopCardinalityLabelNames(labelMaps, maxLabelCols)

	headers := make([]string, 0, 3+len(cols))
	headers = append(headers, "PROFILE ID", "TIMESTAMP", "SCORE")
	for _, c := range cols {
		headers = append(headers, strings.ToUpper(c))
	}

	t := style.NewTable(headers...)
	if len(result.Anomalies) == 0 {
		row := make([]string, len(headers))
		row[0] = "(no anomalies)"
		t.Row(row...)
		return t.Render(w)
	}

	for _, a := range result.Anomalies {
		row := []string{
			a.ProfileID,
			a.Timestamp.UTC().Format(time.RFC3339),
			fmt.Sprintf("%.4f", a.Score),
		}
		for _, c := range cols {
			row = append(row, a.Labels[c])
		}
		t.Row(row...)
	}
	return t.Render(w)
}
