package irm

import (
	"fmt"
	"io"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// workingHoursGetOpts holds the flags of get-working-hours. The command prints
// a weekly grid by default; yaml and json emit the document that
// update-working-hours accepts.
type workingHoursGetOpts struct {
	IO cmdio.Options
}

func (o *workingHoursGetOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, workingHoursTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// workingHoursUpdateOpts holds the flags of update-working-hours. Like
// get-working-hours it prints the stored hours as a weekly grid by default.
type workingHoursUpdateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *workingHoursUpdateOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, workingHoursTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the resource definition (JSON/YAML, use - for stdin)")
}

// encodeWorkingHours writes a working-hours document in the format that the
// command asked for: the weekly grid for "table", the document itself for
// every other format.
func encodeWorkingHours(opts *cmdio.Options, w io.Writer, doc map[string]any) error {
	if opts.OutputFormat == "table" {
		return opts.Encode(w, workingHoursRows(doc))
	}
	return opts.Encode(w, doc)
}

// workingHoursRow is one interval of a team's business hours, with the time
// ranges of each weekday rendered for display.
type workingHoursRow struct {
	Name string
	// Days holds the rendered ranges per weekday, indexed like workingHoursDisplayDays.
	Days [7]string
}

// workingHoursDisplayDays orders the grid columns Monday to Sunday and maps
// each to its weekday key in the document.
//
//nolint:gochecknoglobals // a lookup table, written once at package level
var workingHoursDisplayDays = [7]struct{ Header, Key string }{
	{"MON", "monday"}, {"TUE", "tuesday"}, {"WED", "wednesday"}, {"THU", "thursday"},
	{"FRI", "friday"}, {"SAT", "saturday"}, {"SUN", "sunday"},
}

func workingHoursTable() cmdio.Table[workingHoursRow] {
	columns := make([]cmdio.Column[workingHoursRow], 0, 1+len(workingHoursDisplayDays))
	columns = append(columns, cmdio.Column[workingHoursRow]{
		Header:  "INTERVAL",
		Content: func(r workingHoursRow) string { return orDash(r.Name) },
	})
	for i, day := range workingHoursDisplayDays {
		columns = append(columns, cmdio.Column[workingHoursRow]{
			Header:  day.Header,
			Content: func(r workingHoursRow) string { return orDash(r.Days[i]) },
		})
	}

	return cmdio.Table[workingHoursRow]{
		Columns: columns,
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No business hours are set for this team.")
			return err
		},
	}
}

// workingHoursRows turns the get-working-hours document into display rows.
// Anything that does not have the expected shape is skipped; the document
// itself stays available through -o yaml and -o json.
func workingHoursRows(doc map[string]any) []workingHoursRow {
	intervals, _ := doc["irm_working_hours"].([]any)

	rows := make([]workingHoursRow, 0, len(intervals))
	for _, raw := range intervals {
		interval, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		var row workingHoursRow
		row.Name, _ = interval["name"].(string)

		week, _ := interval["irm_working_hours"].(map[string]any)
		for i, day := range workingHoursDisplayDays {
			row.Days[i] = formatWorkingHoursRanges(week[day.Key])
		}
		rows = append(rows, row)
	}
	return rows
}

// formatWorkingHoursRanges renders the ranges of one weekday, for example
// "09:00-12:00, 13:00-17:00". A day without ranges renders as "".
func formatWorkingHoursRanges(raw any) string {
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
