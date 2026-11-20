package irm

import (
	"fmt"
	"io"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// teamHoursGetOpts holds the flags of get-team-hours. The command prints
// a weekly grid by default; yaml and json emit the document that
// update-team-hours accepts.
type teamHoursGetOpts struct {
	IO cmdio.Options
}

func (o *teamHoursGetOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, teamHoursTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// teamHoursUpdateOpts holds the flags of update-team-hours. Like
// get-team-hours it prints the stored hours as a weekly grid by default.
type teamHoursUpdateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *teamHoursUpdateOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, teamHoursTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the resource definition (JSON/YAML, use - for stdin)")
}

// encodeTeamHours writes a team-hours document in the format that the
// command asked for: the weekly grid for "table", the document itself for
// every other format.
func encodeTeamHours(opts *cmdio.Options, w io.Writer, doc map[string]any) error {
	if opts.OutputFormat == "table" {
		return opts.Encode(w, teamHoursRows(doc))
	}
	return opts.Encode(w, doc)
}

// teamHoursRow is one interval of a team's business hours, with the time
// ranges of each weekday rendered for display.
type teamHoursRow struct {
	Name string
	// Days holds the rendered ranges per weekday, indexed like teamHoursDisplayDays.
	Days [7]string
}

// teamHoursDisplayDays orders the grid columns Monday to Sunday and maps
// each to its weekday key in the document.
//
//nolint:gochecknoglobals // a lookup table, written once at package level
var teamHoursDisplayDays = [7]struct{ Header, Key string }{
	{"MON", "monday"}, {"TUE", "tuesday"}, {"WED", "wednesday"}, {"THU", "thursday"},
	{"FRI", "friday"}, {"SAT", "saturday"}, {"SUN", "sunday"},
}

func teamHoursTable() cmdio.Table[teamHoursRow] {
	columns := make([]cmdio.Column[teamHoursRow], 0, 1+len(teamHoursDisplayDays))
	columns = append(columns, cmdio.Column[teamHoursRow]{
		Header:  "INTERVAL",
		Content: func(r teamHoursRow) string { return orDash(r.Name) },
	})
	for i, day := range teamHoursDisplayDays {
		columns = append(columns, cmdio.Column[teamHoursRow]{
			Header:  day.Header,
			Content: func(r teamHoursRow) string { return orDash(r.Days[i]) },
		})
	}

	return cmdio.Table[teamHoursRow]{
		Columns: columns,
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No business hours are set for this team.")
			return err
		},
	}
}

// teamHoursRows turns the get-team-hours document into display rows.
// Anything that does not have the expected shape is skipped; the document
// itself stays available through -o yaml and -o json.
func teamHoursRows(doc map[string]any) []teamHoursRow {
	intervals, _ := doc["team_hours"].([]any)

	rows := make([]teamHoursRow, 0, len(intervals))
	for _, raw := range intervals {
		interval, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		var row teamHoursRow
		row.Name, _ = interval["name"].(string)

		week, _ := interval["hours"].(map[string]any)
		for i, day := range teamHoursDisplayDays {
			row.Days[i] = formatTeamHoursRanges(week[day.Key])
		}
		rows = append(rows, row)
	}
	return rows
}

// formatTeamHoursRanges renders the ranges of one weekday, for example
// "09:00-12:00, 13:00-17:00". A day without ranges renders as "".
func formatTeamHoursRanges(raw any) string {
	ranges, _ := raw.([]any)

	parts := make([]string, 0, len(ranges))
	for _, item := range ranges {
		r, ok := item.(map[string]any)
		if !ok {
			continue
		}
		start, _ := r["start"].(string)
		end, _ := r["end"].(string)
		if start == "" || end == "" {
			continue
		}
		parts = append(parts, shortClock(start)+"-"+shortClock(end))
	}
	return strings.Join(parts, ", ")
}

// shortClock drops a zero seconds part: "09:00:00" becomes "09:00".
func shortClock(s string) string {
	if t, err := time.Parse("15:04:05", s); err == nil && t.Second() == 0 {
		return t.Format("15:04")
	}
	return s
}
