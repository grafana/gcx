package errortracking

import (
	"fmt"
	"io"
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
)

type release struct {
	Release    string `json:"release"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
	EventCount int64  `json:"eventCount"`
}

const facetCap = 100

type errorsSection = itemList[errorSummary]

type releaseStatus struct {
	Release   *release      `json:"release"`
	Project   projectInfo   `json:"project"`
	New       errorsSection `json:"new"`
	Regressed errorsSection `json:"regressed"`
	LastSeen  errorsSection `json:"lastSeen"`
}

func writeSection(w io.Writer, title string, s errorsSection, lastSeen bool) error {
	count := strconv.Itoa(len(s.Items))
	if m := s.ListMeta; m != nil && m.Truncated {
		count = "first " + count
	}
	fmt.Fprintf(w, "\n%s (%s)\n", title, count)
	if len(s.Items) == 0 {
		return nil
	}
	keep := []cmdio.Column[errorSummary]{}
	for _, c := range errorColumns(func(e errorSummary) errorSummary { return e }) {
		if c.Visible == cmdio.WideOnly || (lastSeen && c.Header == "RELEASE") || (!lastSeen && c.Header == "STATUS") {
			continue
		}
		keep = append(keep, c)
	}
	return cleanTable(cmdio.Table[errorSummary]{Columns: keep}).Codec(cmdio.FormatTable).Encode(w, s.Items)
}

func writeRelease(w io.Writer, r releaseStatus) error {
	if r.Release == nil {
		return nil
	}
	fmt.Fprintf(w, "Release %s · %s\n", printable(r.Release.Release), projectLabel(r.Project.ID, r.Project.Name))
	fmt.Fprintf(w, "first event %s · last event %s · %d events\n", seenAt(r.Release.FirstSeen), seenAt(r.Release.LastSeen), r.Release.EventCount)
	for _, s := range []struct {
		title    string
		section  errorsSection
		lastSeen bool
	}{
		{"REGRESSED since its first event", r.Regressed, false},
		{"NEW since its first event", r.New, false},
		{"LAST SEEN IN THIS RELEASE", r.LastSeen, true},
	} {
		if err := writeSection(w, s.title, s.section, s.lastSeen); err != nil {
			return err
		}
	}
	return nil
}
