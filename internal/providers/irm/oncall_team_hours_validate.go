package irm

import (
	"fmt"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// teamHoursWeekdays lists the weekday keys of an interval. The backend
// ignores any other key, so a typo would silently turn a day into a day off.
//
//nolint:gochecknoglobals // a lookup table, written once at package level
var teamHoursWeekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// validateTeamHours checks the document that update-team-hours is about to
// send: {"team_hours": null | [interval, ...]}. It only covers what the
// backend cannot report usefully: unknown weekday keys (silently dropped) and
// intervals without any range. The backend validates everything else (limits,
// time formats, ordering and overlap) and answers with a clear 400, so those
// rules are not repeated here where they could drift. It collects every
// problem so that one run reports them all.
func validateTeamHours(doc map[string]any) error {
	var problems []string

	intervals, present := doc["team_hours"]
	switch list := intervals.(type) {
	case nil:
		if !present {
			problems = append(problems, `missing the "team_hours" key (use "team_hours: null" to clear the team's hours)`)
		}
	case []any:
		for i, raw := range list {
			problems = append(problems, validateTeamHoursInterval(fmt.Sprintf("team_hours[%d]", i), raw)...)
		}
	default:
		problems = append(problems, `"team_hours" must be a list of intervals, or null to clear the team's hours`)
	}

	if len(problems) == 0 {
		return nil
	}

	exitCode := gcxerrors.ExitUsageError
	return &gcxerrors.DetailedError{
		Summary:  "Invalid team hours",
		Details:  "- " + strings.Join(problems, "\n- "),
		ExitCode: &exitCode,
		Suggestions: []string{
			"Emit the current document with: gcx irm oncall teams get-team-hours <id>",
		},
	}
}

func validateTeamHoursInterval(where string, raw any) []string {
	interval, ok := raw.(map[string]any)
	if !ok {
		return []string{where + ": must be an object with a name and hours"}
	}

	week, ok := interval["hours"].(map[string]any)
	if !ok {
		if interval["hours"] == nil {
			return []string{where + noTeamHoursRanges}
		}
		return []string{where + `: "hours" must map weekday names to time ranges`}
	}

	var problems []string
	for key := range week {
		if !slices.Contains(teamHoursWeekdays, key) {
			problems = append(problems, fmt.Sprintf("%s: unknown weekday %q (expected one of: %s)",
				where, key, strings.Join(teamHoursWeekdays, ", ")))
		}
	}

	// Report the missing ranges only when nothing else is wrong with the week: a typo'd weekday
	// already explains why no range was found.
	if len(problems) == 0 && !hasTeamHoursRange(week) {
		problems = append(problems, where+noTeamHoursRanges)
	}

	return problems
}

const noTeamHoursRanges = ": needs at least one time range on at least one weekday " +
	"(the backend stores an interval as one row per weekday with ranges, so an interval without any is dropped)"

func hasTeamHoursRange(week map[string]any) bool {
	for _, raw := range week {
		if ranges, ok := raw.([]any); ok && len(ranges) > 0 {
			return true
		}
	}
	return false
}
