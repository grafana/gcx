package pyroscope

import (
	"io"
	"sort"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// ProfileExemplarsResult is the processed result of a profile-exemplars query
// (SelectSeries + EXEMPLAR_TYPE_INDIVIDUAL), flattened and sorted by value desc.
type ProfileExemplarsResult struct {
	From        time.Time         `json:"from"`
	To          time.Time         `json:"to"`
	ProfileType string            `json:"profileType"`
	Exemplars   []ProfileExemplar `json:"exemplars"`
}

// ProfileExemplar is a single flattened profile-exemplar entry.
type ProfileExemplar struct {
	ProfileID string            `json:"profileId"`
	Timestamp time.Time         `json:"timestamp"`
	Value     int64             `json:"value"`
	SpanID    string            `json:"spanId,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// SpanExemplarsResult is the processed result of a span-exemplars query
// (SelectHeatmap + HEATMAP_QUERY_TYPE_SPAN + EXEMPLAR_TYPE_SPAN).
type SpanExemplarsResult struct {
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	ProfileType string         `json:"profileType"`
	Exemplars   []SpanExemplar `json:"exemplars"`
}

// SpanExemplar is a single flattened span-exemplar entry.
type SpanExemplar struct {
	SpanID    string            `json:"spanId"`
	Timestamp time.Time         `json:"timestamp"`
	Value     int64             `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// BuildProfileExemplarsResult flattens SelectSeriesResponse exemplars into a
// sorted, truncated ProfileExemplarsResult. Entries are sorted by value
// descending and truncated to topN (topN<=0 disables truncation).
func BuildProfileExemplarsResult(resp *SelectSeriesResponse, from, to time.Time, profileType string, topN int) *ProfileExemplarsResult {
	total := 0
	for _, s := range resp.Series {
		for _, p := range s.Points {
			total += len(p.Exemplars)
		}
	}
	entries := make([]ProfileExemplar, 0, total)
	for _, s := range resp.Series {
		seriesLabels := labelPairsToMap(s.Labels)
		for _, p := range s.Points {
			for _, ex := range p.Exemplars {
				entries = append(entries, ProfileExemplar{
					ProfileID: ex.ProfileID,
					Timestamp: time.UnixMilli(ex.TimestampMs()).UTC(),
					Value:     ex.Int64Value(),
					SpanID:    ex.SpanID,
					Labels:    mergeLabels(seriesLabels, ex.Labels),
				})
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Value > entries[j].Value })
	if topN > 0 && len(entries) > topN {
		entries = entries[:topN]
	}

	return &ProfileExemplarsResult{
		From:        from,
		To:          to,
		ProfileType: profileType,
		Exemplars:   entries,
	}
}

// BuildSpanExemplarsResult flattens SelectHeatmapResponse exemplars. Entries
// without a SpanID are skipped (mirrors profilecli: span exemplars without a
// span ID aren't actionable).
func BuildSpanExemplarsResult(resp *SelectHeatmapResponse, from, to time.Time, profileType string, topN int) *SpanExemplarsResult {
	total := 0
	for _, s := range resp.Series {
		for _, slot := range s.Slots {
			total += len(slot.Exemplars)
		}
	}
	entries := make([]SpanExemplar, 0, total)
	for _, s := range resp.Series {
		seriesLabels := labelPairsToMap(s.Labels)
		for _, slot := range s.Slots {
			for _, ex := range slot.Exemplars {
				if ex.SpanID == "" {
					continue
				}
				entries = append(entries, SpanExemplar{
					SpanID:    ex.SpanID,
					Timestamp: time.UnixMilli(ex.TimestampMs()).UTC(),
					Value:     ex.Int64Value(),
					Labels:    mergeLabels(seriesLabels, ex.Labels),
				})
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Value > entries[j].Value })
	if topN > 0 && len(entries) > topN {
		entries = entries[:topN]
	}

	return &SpanExemplarsResult{
		From:        from,
		To:          to,
		ProfileType: profileType,
		Exemplars:   entries,
	}
}

// TopCardinalityLabelNames returns up to n label names with the highest
// cardinality across the provided label maps, excluding internal __..__
// labels. Ties break alphabetically for stable output.
func TopCardinalityLabelNames(labelMaps []map[string]string, n int) []string {
	if len(labelMaps) == 0 || n <= 0 {
		return nil
	}

	distinct := make(map[string]map[string]struct{})
	for _, lbls := range labelMaps {
		for k, v := range lbls {
			if isInternalLabel(k) {
				continue
			}
			if distinct[k] == nil {
				distinct[k] = make(map[string]struct{})
			}
			distinct[k][v] = struct{}{}
		}
	}

	type cand struct {
		name  string
		count int
	}
	cands := make([]cand, 0, len(distinct))
	for name, vs := range distinct {
		cands = append(cands, cand{name: name, count: len(vs)})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].count != cands[j].count {
			return cands[i].count > cands[j].count
		}
		return cands[i].name < cands[j].name
	})

	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.name
	}
	return out
}

// FormatProfileExemplarsTable renders a ProfileExemplarsResult as a table.
// maxLabelCols auto-selects the N highest-cardinality label columns to show
// (0 hides label columns entirely).
func FormatProfileExemplarsTable(w io.Writer, result *ProfileExemplarsResult, maxLabelCols int) error {
	unit := sampleUnitFromProfileType(result.ProfileType)

	labelMaps := make([]map[string]string, len(result.Exemplars))
	for i, e := range result.Exemplars {
		labelMaps[i] = e.Labels
	}
	cols := TopCardinalityLabelNames(labelMaps, maxLabelCols)

	hasSpanID := false
	for _, e := range result.Exemplars {
		if e.SpanID != "" {
			hasSpanID = true
			break
		}
	}

	headers := []string{"PROFILE ID", "TIMESTAMP", "VALUE (" + strings.ToUpper(unit) + ")"}
	if hasSpanID {
		headers = append(headers, "SPAN ID")
	}
	for _, c := range cols {
		headers = append(headers, strings.ToUpper(c))
	}

	t := style.NewTable(headers...)
	if len(result.Exemplars) == 0 {
		row := make([]string, len(headers))
		row[0] = "(no exemplars)"
		t.Row(row...)
		return t.Render(w)
	}

	for _, e := range result.Exemplars {
		row := []string{
			e.ProfileID,
			e.Timestamp.UTC().Format(time.RFC3339),
			formatHumanValue(float64(e.Value), unit),
		}
		if hasSpanID {
			row = append(row, e.SpanID)
		}
		for _, c := range cols {
			row = append(row, e.Labels[c])
		}
		t.Row(row...)
	}
	return t.Render(w)
}

// FormatSpanExemplarsTable renders a SpanExemplarsResult as a table.
func FormatSpanExemplarsTable(w io.Writer, result *SpanExemplarsResult, maxLabelCols int) error {
	unit := sampleUnitFromProfileType(result.ProfileType)

	labelMaps := make([]map[string]string, len(result.Exemplars))
	for i, e := range result.Exemplars {
		labelMaps[i] = e.Labels
	}
	cols := TopCardinalityLabelNames(labelMaps, maxLabelCols)

	headers := []string{"SPAN ID", "TIMESTAMP", "VALUE (" + strings.ToUpper(unit) + ")"}
	for _, c := range cols {
		headers = append(headers, strings.ToUpper(c))
	}

	t := style.NewTable(headers...)
	if len(result.Exemplars) == 0 {
		row := make([]string, len(headers))
		row[0] = "(no span exemplars)"
		t.Row(row...)
		return t.Render(w)
	}

	for _, e := range result.Exemplars {
		row := []string{
			e.SpanID,
			e.Timestamp.UTC().Format(time.RFC3339),
			formatHumanValue(float64(e.Value), unit),
		}
		for _, c := range cols {
			row = append(row, e.Labels[c])
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func labelPairsToMap(lps []LabelPair) map[string]string {
	m := make(map[string]string, len(lps))
	for _, lp := range lps {
		if !isInternalLabel(lp.Name) {
			m[lp.Name] = lp.Value
		}
	}
	return m
}

// mergeLabels copies seriesLabels and overlays exemplar-level labels, skipping
// internal __..__ labels. Allocates a new map so callers can't mutate shared state.
func mergeLabels(seriesLabels map[string]string, extra []LabelPair) map[string]string {
	out := make(map[string]string, len(seriesLabels)+len(extra))
	for k, v := range seriesLabels {
		out[k] = v
	}
	for _, lp := range extra {
		if !isInternalLabel(lp.Name) {
			out[lp.Name] = lp.Value
		}
	}
	return out
}

func isInternalLabel(name string) bool {
	return len(name) >= 4 && strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__")
}
