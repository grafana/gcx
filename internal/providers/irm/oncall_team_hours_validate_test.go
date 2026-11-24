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
	return map[string]any{"name": name, "hours": week}
}

func doc(intervals ...any) map[string]any {
	return map[string]any{"team_hours": intervals}
}

func TestValidateTeamHoursDoesNotDuplicateTheMissingRangeMessage(t *testing.T) {
	t.Parallel()

	// "funday" explains why no range was found, so the generic message would only add noise.
	err := validateTeamHours(doc(interval("Office", map[string]any{"funday": day([2]string{"09:00", "17:00"})})))

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

func TestValidateTeamHours(t *testing.T) {
	t.Parallel()

	nineToFive := day([2]string{"09:00:00", "17:00:00"})

	tests := []struct {
		name string
		doc  map[string]any
		want []string // substrings that every reported problem set must contain; empty means valid
	}{
		{name: "valid week", doc: doc(interval("Office", map[string]any{"monday": nineToFive, "sunday": []any{}}))},
		{name: "clear with null", doc: map[string]any{"team_hours": nil}},
		{name: "clear with empty list", doc: map[string]any{"team_hours": []any{}}},
		// The backend owns these rules and answers with a 400, so they must pass through.
		{name: "backend-validated rules pass through", doc: doc(interval("", map[string]any{
			"monday": day([2]string{"17:00:00", "09:00:00"}, [2]string{"9am", "10:00"}),
		}))},

		{name: "missing key", doc: map[string]any{"other": 1}, want: []string{`missing the "team_hours" key`}},
		{name: "old envelope key", doc: map[string]any{"irm_working_hours": []any{}}, want: []string{`missing the "team_hours" key`}},
		{name: "wrong type", doc: map[string]any{"team_hours": "x"}, want: []string{"must be a list of intervals"}},
		{name: "interval not an object", doc: doc("monday"), want: []string{"team_hours[0]: must be an object"}},
		{name: "unknown weekday", doc: doc(interval("Office", map[string]any{"funday": nineToFive})),
			want: []string{`unknown weekday "funday"`, "sunday, monday"}},
		{name: "week not an object", doc: doc(interval("Office", nil), map[string]any{"name": "b", "hours": []any{}}),
			want: []string{`team_hours[1]: "hours" must map weekday names`}},
		{name: "old interval key", doc: doc(map[string]any{"name": "Office", "irm_working_hours": map[string]any{"monday": nineToFive}}),
			want: []string{"team_hours[0]: needs at least one time range"}},
		{name: "interval without days", doc: doc(map[string]any{"name": "Empty", "hours": nil}),
			want: []string{"team_hours[0]: needs at least one time range"}},
		{name: "interval with every day off", doc: doc(interval("Holiday", map[string]any{
			"monday": []any{}, "tuesday": nil, "sunday": []any{},
		})), want: []string{"team_hours[0]: needs at least one time range"}},
		{name: "interval with an empty week", doc: doc(interval("Holiday", map[string]any{})),
			want: []string{"needs at least one time range"}},
		{name: "empty interval beside a valid one", doc: doc(
			interval("Office", map[string]any{"monday": nineToFive}),
			interval("Holiday", map[string]any{"monday": []any{}}),
		), want: []string{"team_hours[1]: needs at least one time range"}},
		{name: "reports every problem", doc: doc(
			interval("a", map[string]any{"funday": nineToFive}),
			interval("b", map[string]any{"holiday": nineToFive}),
		), want: []string{`team_hours[0]: unknown weekday "funday"`, `team_hours[1]: unknown weekday "holiday"`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateTeamHours(tt.doc)
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
