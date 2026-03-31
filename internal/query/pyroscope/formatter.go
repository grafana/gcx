package pyroscope

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// FormatQueryTable formats a Pyroscope query response as a table showing top functions.
func FormatQueryTable(w io.Writer, resp *QueryResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FUNCTION\tSELF\tTOTAL\tPERCENTAGE")

	if resp.Flamegraph == nil || len(resp.Flamegraph.Names) == 0 {
		fmt.Fprintln(tw, "(no profile data)")
		return tw.Flush()
	}

	samples := ExtractTopFunctions(resp.Flamegraph, 20)

	for _, s := range samples {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.2f%%\n",
			truncateString(s.Name, 60),
			s.Self,
			s.Total,
			s.Percentage)
	}

	return tw.Flush()
}

// FormatProfileTypesTable formats profile types as a table.
func FormatProfileTypesTable(w io.Writer, resp *ProfileTypesResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSAMPLE_TYPE\tSAMPLE_UNIT")

	for _, pt := range resp.ProfileTypes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			pt.ID,
			pt.Name,
			pt.SampleType,
			pt.SampleUnit)
	}

	return tw.Flush()
}

// FormatLabelsTable formats label names or values as a table.
func FormatLabelsTable(w io.Writer, labels []string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL")

	for _, label := range labels {
		fmt.Fprintln(tw, label)
	}

	return tw.Flush()
}

// ExtractTopFunctions extracts the top N functions by self time from a flame graph.
func ExtractTopFunctions(fg *Flamegraph, limit int) []FunctionSample {
	if fg == nil || len(fg.Levels) == 0 {
		return nil
	}

	// Build a map of function name -> aggregated stats
	funcStats := make(map[string]*FunctionSample)

	// Flame graph levels have values in groups of 4: [offset, total, self, nameIndex]
	for _, level := range fg.Levels {
		for i := 0; i+3 < len(level.Values); i += 4 {
			nameIdx, err := parseInt64(level.Values[i+3])
			if err != nil || nameIdx < 0 || int(nameIdx) >= len(fg.Names) {
				continue
			}
			name := fg.Names[nameIdx]

			// Skip "other" entries
			if name == "other" {
				continue
			}

			total, err := parseInt64(level.Values[i+1])
			if err != nil {
				continue
			}
			self, err := parseInt64(level.Values[i+2])
			if err != nil {
				continue
			}

			if existing, ok := funcStats[name]; ok {
				existing.Self += self
				existing.Total += total
			} else {
				funcStats[name] = &FunctionSample{
					Name:  name,
					Self:  self,
					Total: total,
				}
			}
		}
	}

	// Convert to slice and calculate percentages
	samples := make([]FunctionSample, 0, len(funcStats))
	for _, s := range funcStats {
		if fg.Total > 0 {
			s.Percentage = float64(s.Self) / float64(fg.Total) * 100
		}
		samples = append(samples, *s)
	}

	// Sort by self time descending
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].Self > samples[j].Self
	})

	// Limit results
	if len(samples) > limit {
		samples = samples[:limit]
	}

	return samples
}

// FormatSeriesTable formats a SelectSeries response as a table with one row per data point.
func FormatSeriesTable(w io.Writer, resp *SelectSeriesResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "LABELS\tTIMESTAMP\tVALUE")

	if len(resp.Series) == 0 {
		fmt.Fprintln(tw, "(no series data)")
		return tw.Flush()
	}

	for _, s := range resp.Series {
		labels := FormatLabelPairs(s.Labels)
		for _, p := range s.Points {
			ts := time.UnixMilli(p.TimestampMs()).UTC().Format(time.RFC3339)
			fmt.Fprintf(tw, "%s\t%s\t%.2f\n", labels, ts, p.FloatValue())
		}
	}

	return tw.Flush()
}

// FormatSeriesTableWide formats a SelectSeries response with label pairs exploded into columns.
func FormatSeriesTableWide(w io.Writer, resp *SelectSeriesResponse) error {
	if len(resp.Series) == 0 {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "TIMESTAMP\tVALUE")
		fmt.Fprintln(tw, "(no series data)")
		return tw.Flush()
	}

	// Collect all unique label names across all series (sorted for stable output).
	labelNameSet := make(map[string]struct{})
	for _, s := range resp.Series {
		for _, lp := range s.Labels {
			labelNameSet[lp.Name] = struct{}{}
		}
	}
	labelNames := make([]string, 0, len(labelNameSet))
	for name := range labelNameSet {
		labelNames = append(labelNames, name)
	}
	sort.Strings(labelNames)

	// Build header.
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	header := make([]string, 0, len(labelNames)+2)
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "TIMESTAMP", "VALUE")
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	// Write rows.
	for _, s := range resp.Series {
		labelMap := make(map[string]string, len(s.Labels))
		for _, lp := range s.Labels {
			labelMap[lp.Name] = lp.Value
		}
		for _, p := range s.Points {
			row := make([]string, 0, len(labelNames)+2)
			for _, name := range labelNames {
				row = append(row, labelMap[name])
			}
			ts := time.UnixMilli(p.TimestampMs()).UTC().Format(time.RFC3339)
			row = append(row, ts, fmt.Sprintf("%.2f", p.FloatValue()))
			fmt.Fprintln(tw, strings.Join(row, "\t"))
		}
	}

	return tw.Flush()
}

// FormatLabelPairs formats label pairs as {key=val, key=val, ...}.
func FormatLabelPairs(labels []LabelPair) string {
	if len(labels) == 0 {
		return "{}"
	}
	parts := make([]string, len(labels))
	for i, lp := range labels {
		parts[i] = lp.Name + "=" + lp.Value
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// FormatTopSeriesTable formats a TopSeriesResponse as a ranked leaderboard table.
func FormatTopSeriesTable(w io.Writer, resp *TopSeriesResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if len(resp.Series) == 0 {
		fmt.Fprintln(tw, "RANK\tLABELS\tTOTAL")
		fmt.Fprintln(tw, "(no series data)")
		return tw.Flush()
	}

	// Detect sample unit from profile type for human-readable formatting.
	sampleUnit := sampleUnitFromProfileType(resp.ProfileType)

	// Build header with group-by label names as columns.
	if len(resp.GroupBy) > 0 {
		var hdr strings.Builder
		hdr.WriteString("RANK")
		for _, name := range resp.GroupBy {
			hdr.WriteString("\t")
			hdr.WriteString(strings.ToUpper(name))
		}
		hdr.WriteString("\tTOTAL (")
		hdr.WriteString(strings.ToUpper(sampleUnit))
		hdr.WriteString(")")
		fmt.Fprintln(tw, hdr.String())

		for _, e := range resp.Series {
			var row strings.Builder
			row.WriteString(strconv.Itoa(e.Rank))
			for _, name := range resp.GroupBy {
				v := e.Labels[name]
				if v == "" {
					v = "<unknown>"
				}
				row.WriteString("\t")
				row.WriteString(v)
			}
			row.WriteString("\t")
			row.WriteString(formatHumanValue(e.Total, sampleUnit))
			fmt.Fprintln(tw, row.String())
		}
	} else {
		fmt.Fprintln(tw, "RANK\tLABELS\tTOTAL ("+strings.ToUpper(sampleUnit)+")")
		for _, e := range resp.Series {
			labels := FormatLabelsMap(e.Labels)
			fmt.Fprintf(tw, "%d\t%s\t%s\n", e.Rank, labels, formatHumanValue(e.Total, sampleUnit))
		}
	}

	return tw.Flush()
}

// sampleUnitFromProfileType extracts the sample unit from a profile type ID.
// Format: name:sample_type:sample_unit:period_type:period_unit.
func sampleUnitFromProfileType(profileType string) string {
	parts := strings.Split(profileType, ":")
	if len(parts) >= 3 {
		return parts[2]
	}
	return "samples"
}

// formatHumanValue formats a value with human-readable units.
func formatHumanValue(v float64, unit string) string {
	switch unit {
	case "nanoseconds":
		return time.Duration(int64(v)).String()
	case "bytes":
		return formatBytes(uint64(v))
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatLabelsMap formats a map of labels as {key=val, key=val, ...}.
func FormatLabelsMap(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + labels[k]
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
