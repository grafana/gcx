//nolint:testpackage // white-box tests require access to the unexported validator
package irm

import (
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func day(ranges ...[2]string) []any {
	out := make([]any, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, map[string]any{"start": r[0], "end": r[1]})
	}
	return out
}

func interval(name any, week map[string]any) map[string]any {
	return map[string]any{"name": name, "irm_working_hours": week}
}

func doc(intervals ...any) map[string]any {
	return map[string]any{"irm_working_hours": intervals}
}

func TestValidateTeamWorkingHoursDoesNotDuplicateTheMissingRangeMessage(t *testing.T) {
	t.Parallel()

	// "funday" explains why no range was found, so the generic message would only add noise.
	err := validateTeamWorkingHours(doc(interval("Office", map[string]any{"funday": day([2]string{"09:00", "17:00"})})))

	var detailed *gcxerrors.DetailedError
	if !errors.As(err, &detailed) {
		t.Fatalf("expected a DetailedError, got %T: %v", err, err)
	}
	if !strings.Contains(detailed.Details, `unknown weekday "funday"`) {
		t.Errorf("expected the weekday problem, got:\n%s", detailed.Details)
	}
	if strings.Contains(detailed.Details, "needs at least one time range") {
		t.Errorf("the missing-range message should not repeat a typo that already explains it:\n%s", detailed.Details)
	}
}

func TestValidateTeamWorkingHours(t *testing.T) {
	t.Parallel()

	nineToFive := day([2]string{"09:00:00", "17:00:00"})

	tests := []struct {
		name string
		doc  map[string]any
		want []string // substrings that every reported problem set must contain; empty means valid
	}{
		{name: "valid week", doc: doc(interval("Office", map[string]any{"monday": nineToFive, "sunday": []any{}}))},
		{name: "clear with null", doc: map[string]any{"irm_working_hours": nil}},
		{name: "clear with empty list", doc: map[string]any{"irm_working_hours": []any{}}},
		{name: "minutes only times", doc: doc(interval("Office", map[string]any{"monday": day([2]string{"09:00", "17:30"})}))},
		{name: "adjacent ranges", doc: doc(interval("Split", map[string]any{
			"monday": day([2]string{"09:00:00", "12:00:00"}, [2]string{"12:00:00", "17:00:00"}),
		}))},
		{name: "three ranges", doc: doc(interval("Split", map[string]any{
			"monday": day([2]string{"06:00:00", "08:00:00"}, [2]string{"09:00:00", "12:00:00"}, [2]string{"13:00:00", "17:00:00"}),
		}))},

		{name: "missing key", doc: map[string]any{"other": 1}, want: []string{`missing the "irm_working_hours" key`}},
		{name: "wrong type", doc: map[string]any{"irm_working_hours": "x"}, want: []string{"must be a list of intervals"}},
		{name: "too many intervals", doc: doc(
			interval("a", nil), interval("b", nil), interval("c", nil), interval("d", nil),
		), want: []string{"at most 3 intervals, got 4"}},
		{name: "interval not an object", doc: doc("monday"), want: []string{"irm_working_hours[0]: must be an object"}},
		{name: "missing name", doc: doc(map[string]any{"irm_working_hours": nil}), want: []string{`"name" is required`}},
		{name: "blank name", doc: doc(interval("  ", nil)), want: []string{`"name" must not be empty`}},
		{name: "long name", doc: doc(interval(strings.Repeat("x", 101), nil)), want: []string{"at most 100 characters"}},
		{name: "unknown weekday", doc: doc(interval("Office", map[string]any{"funday": nineToFive})),
			want: []string{`unknown weekday "funday"`, "sunday, monday"}},
		{name: "week not an object", doc: doc(interval("Office", nil), map[string]any{"name": "b", "irm_working_hours": []any{}}),
			want: []string{"irm_working_hours[1]: \"irm_working_hours\" must map weekday names"}},
		{name: "day not a list", doc: doc(interval("Office", map[string]any{"monday": "9-5"})),
			want: []string{"irm_working_hours[0].monday: must be a list"}},
		{name: "too many ranges", doc: doc(interval("Office", map[string]any{
			"monday": day([2]string{"01:00", "02:00"}, [2]string{"03:00", "04:00"}, [2]string{"05:00", "06:00"}, [2]string{"07:00", "08:00"}),
		})), want: []string{"at most 3 time ranges per day, got 4"}},
		{name: "end before start", doc: doc(interval("Office", map[string]any{"monday": day([2]string{"17:00:00", "09:00:00"})})),
			want: []string{"irm_working_hours[0].monday[0]: start (17:00:00) must be before end (09:00:00)"}},
		{name: "start equals end", doc: doc(interval("Office", map[string]any{"monday": day([2]string{"09:00:00", "09:00:00"})})),
			want: []string{"must be before end"}},
		{name: "overlap", doc: doc(interval("Office", map[string]any{
			"monday": day([2]string{"09:00:00", "12:00:00"}, [2]string{"11:00:00", "13:00:00"}),
		})), want: []string{"monday[1]: starts at 11:00:00, before the previous range ends at 12:00:00"}},
		{name: "out of order", doc: doc(interval("Office", map[string]any{
			"monday": day([2]string{"13:00:00", "17:00:00"}, [2]string{"09:00:00", "12:00:00"}),
		})), want: []string{"must be in order and must not overlap"}},
		{name: "bad time format", doc: doc(interval("Office", map[string]any{"monday": day([2]string{"9am", "17:00:00"})})),
			want: []string{`start "9am" is not a valid 24-hour time`}},
		{name: "24 oclock", doc: doc(interval("Office", map[string]any{"monday": day([2]string{"09:00:00", "24:00:00"})})),
			want: []string{`end "24:00:00" is not a valid 24-hour time`}},
		{name: "unquoted time", doc: doc(interval("Office", map[string]any{"monday": []any{map[string]any{"start": 32400, "end": "17:00:00"}}})),
			want: []string{"must be a quoted time string"}},
		{name: "missing end", doc: doc(interval("Office", map[string]any{"monday": []any{map[string]any{"start": "09:00:00"}}})),
			want: []string{"end is required"}},
		{name: "range not an object", doc: doc(interval("Office", map[string]any{"monday": []any{"09:00-17:00"}})),
			want: []string{`must be an object with "start" and "end"`}},
		{name: "interval without days", doc: doc(map[string]any{"name": "Empty", "irm_working_hours": nil}),
			want: []string{"irm_working_hours[0]: needs at least one time range"}},
		{name: "interval with every day off", doc: doc(interval("Holiday", map[string]any{
			"monday": []any{}, "tuesday": nil, "sunday": []any{},
		})), want: []string{"irm_working_hours[0]: needs at least one time range"}},
		{name: "interval with an empty week", doc: doc(interval("Holiday", map[string]any{})),
			want: []string{"needs at least one time range"}},
		{name: "empty interval beside a valid one", doc: doc(
			interval("Office", map[string]any{"monday": nineToFive}),
			interval("Holiday", map[string]any{"monday": []any{}}),
		), want: []string{"irm_working_hours[1]: needs at least one time range"}},
		{name: "reports every problem", doc: doc(interval("", map[string]any{
			"funday": nineToFive,
			"monday": day([2]string{"17:00:00", "09:00:00"}),
		})), want: []string{`"name" must not be empty`, `unknown weekday "funday"`, "must be before end"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateTeamWorkingHours(tt.doc)
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("expected a valid document, got: %v", err)
				}
				return
			}

			var detailed *gcxerrors.DetailedError
			if !errors.As(err, &detailed) {
				t.Fatalf("expected a DetailedError, got %T: %v", err, err)
			}
			if detailed.ExitCode == nil || *detailed.ExitCode != gcxerrors.ExitUsageError {
				t.Errorf("expected the usage exit code, got %v", detailed.ExitCode)
			}
			for _, want := range tt.want {
				if !strings.Contains(detailed.Details, want) {
					t.Errorf("details missing %q:\n%s", want, detailed.Details)
				}
			}
		})
	}
}
