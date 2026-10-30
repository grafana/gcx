package irm

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// Limits that the IRM backend enforces on a team's business hours. Checking
// them here turns a nested JSON error from the server into a message that
// names the offending interval, weekday and range.
const (
	maxTeamWorkingHoursIntervals = 3
	maxWorkingHoursRangesPerDay  = 3
	maxWorkingHoursIntervalName  = 100
)

// workingHoursWeekdays lists the weekday keys of an interval. The backend
// ignores any other key, so a typo would silently turn a day into a day off.
//
//nolint:gochecknoglobals // a lookup table, written once at package level
var workingHoursWeekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// workingHoursTimeLayouts are the clock formats that the backend accepts.
//
//nolint:gochecknoglobals // a lookup table, written once at package level
var workingHoursTimeLayouts = []string{"15:04:05", "15:04"}

// validateTeamWorkingHours checks the document that update-working-hours is
// about to send: {"irm_working_hours": null | [interval, ...]}. It collects
// every problem so that one run reports them all.
func validateTeamWorkingHours(doc map[string]any) error {
	var problems []string

	intervals, present := doc["irm_working_hours"]
	switch list := intervals.(type) {
	case nil:
		if !present {
			problems = append(problems, `missing the "irm_working_hours" key (use "irm_working_hours: null" to clear the team's hours)`)
		}
	case []any:
		problems = append(problems, validateWorkingHoursIntervals(list)...)
	default:
		problems = append(problems, `"irm_working_hours" must be a list of intervals, or null to clear the team's hours`)
	}

	if len(problems) == 0 {
		return nil
	}

	exitCode := gcxerrors.ExitUsageError
	return &gcxerrors.DetailedError{
		Summary:  "Invalid team working hours",
		Details:  "- " + strings.Join(problems, "\n- "),
		ExitCode: &exitCode,
		Suggestions: []string{
			"Emit the current document with: gcx irm oncall teams get-working-hours <id>",
			"Use 24-hour HH:MM:SS times; each range's start must be before its end",
		},
	}
}

func validateWorkingHoursIntervals(intervals []any) []string {
	var problems []string

	if len(intervals) > maxTeamWorkingHoursIntervals {
		problems = append(problems, fmt.Sprintf("a team can have at most %d intervals, got %d",
			maxTeamWorkingHoursIntervals, len(intervals)))
	}

	for i, raw := range intervals {
		where := fmt.Sprintf("irm_working_hours[%d]", i)

		interval, ok := raw.(map[string]any)
		if !ok {
			problems = append(problems, where+": must be an object with a name and irm_working_hours")
			continue
		}

		switch name := interval["name"].(type) {
		case string:
			switch {
			case strings.TrimSpace(name) == "":
				problems = append(problems, where+`: "name" must not be empty`)
			case len([]rune(name)) > maxWorkingHoursIntervalName:
				problems = append(problems, fmt.Sprintf("%s: \"name\" must be at most %d characters", where, maxWorkingHoursIntervalName))
			}
		default:
			problems = append(problems, where+`: "name" is required and must be a string`)
		}

		problems = append(problems, validateWorkingHoursWeek(where, interval["irm_working_hours"])...)
	}

	return problems
}

func validateWorkingHoursWeek(where string, raw any) []string {
	const noRanges = ": needs at least one time range on at least one weekday " +
		"(the backend stores an interval as one row per weekday with ranges, so an interval without any is dropped)"

	if raw == nil {
		return []string{where + noRanges}
	}

	week, ok := raw.(map[string]any)
	if !ok {
		return []string{where + `: "irm_working_hours" must map weekday names to time ranges`}
	}

	var problems []string
	for key := range week {
		if !isWorkingHoursWeekday(key) {
			problems = append(problems, fmt.Sprintf("%s: unknown weekday %q (expected one of: %s)",
				where, key, strings.Join(workingHoursWeekdays, ", ")))
		}
	}

	hasRange := false
	for _, day := range workingHoursWeekdays {
		raw, ok := week[day]
		if !ok || raw == nil {
			continue
		}
		if ranges, isList := raw.([]any); isList && len(ranges) > 0 {
			hasRange = true
		}
		problems = append(problems, validateWorkingHoursDay(where+"."+day, raw)...)
	}

	// Report the missing ranges only when nothing else is wrong with the week: a typo'd weekday or
	// a malformed day already explains why no range was found.
	if len(problems) == 0 && !hasRange {
		problems = append(problems, where+noRanges)
	}

	return problems
}

func validateWorkingHoursDay(where string, raw any) []string {
	ranges, ok := raw.([]any)
	if !ok {
		return []string{where + `: must be a list of {start, end} ranges`}
	}

	var problems []string
	if len(ranges) > maxWorkingHoursRangesPerDay {
		problems = append(problems, fmt.Sprintf("%s: at most %d time ranges per day, got %d",
			where, maxWorkingHoursRangesPerDay, len(ranges)))
	}

	var previousEnd time.Time
	var previousEndSet bool
	for i, item := range ranges {
		at := fmt.Sprintf("%s[%d]", where, i)

		r, ok := item.(map[string]any)
		if !ok {
			problems = append(problems, at+`: must be an object with "start" and "end"`)
			continue
		}

		start, startErr := parseWorkingHoursTime(r["start"])
		end, endErr := parseWorkingHoursTime(r["end"])
		if startErr != "" {
			problems = append(problems, fmt.Sprintf("%s: start %s", at, startErr))
		}
		if endErr != "" {
			problems = append(problems, fmt.Sprintf("%s: end %s", at, endErr))
		}
		if startErr != "" || endErr != "" {
			continue
		}

		if !start.Before(end) {
			problems = append(problems, fmt.Sprintf("%s: start (%s) must be before end (%s)",
				at, start.Format("15:04:05"), end.Format("15:04:05")))
			continue
		}
		if previousEndSet && start.Before(previousEnd) {
			problems = append(problems, fmt.Sprintf("%s: starts at %s, before the previous range ends at %s; ranges must be in order and must not overlap",
				at, start.Format("15:04:05"), previousEnd.Format("15:04:05")))
		}
		previousEnd, previousEndSet = end, true
	}

	return problems
}

// parseWorkingHoursTime parses a clock time. It returns a description of the
// problem instead of an error so that callers can embed it in a message.
func parseWorkingHoursTime(raw any) (time.Time, string) {
	if raw == nil {
		return time.Time{}, "is required (HH:MM:SS)"
	}

	text, ok := raw.(string)
	if !ok {
		return time.Time{}, fmt.Sprintf("must be a quoted time string such as \"09:00:00\", got %v", raw)
	}
	if text == "" {
		return time.Time{}, "is required (HH:MM:SS)"
	}

	for _, layout := range workingHoursTimeLayouts {
		if t, err := time.Parse(layout, text); err == nil {
			return t, ""
		}
	}
	return time.Time{}, fmt.Sprintf("%q is not a valid 24-hour time (use HH:MM:SS)", text)
}

func isWorkingHoursWeekday(key string) bool {
	return slices.Contains(workingHoursWeekdays, key)
}
