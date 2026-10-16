package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// escalationStepPauseUntilTeamWorkingHours is the step value of "Pause
// escalation until team's working hours". The step waits for the business
// hours of the team that notify_to_team_members names.
const escalationStepPauseUntilTeamWorkingHours = 27

// validateEscalationPolicy checks a policy before it is sent. The backend
// accepts a pause step without a team, and the step then fails every time an
// alert group reaches it, so the missing team is caught here instead.
func validateEscalationPolicy(p *EscalationPolicy) error {
	step, ok := escalationStepValue(p.Step)
	if !ok || step != escalationStepPauseUntilTeamWorkingHours {
		return nil
	}

	if teamID(p) != "" {
		return nil
	}

	exitCode := gcxerrors.ExitUsageError
	return &gcxerrors.DetailedError{
		Summary: "Escalation policy needs a team",
		Details: fmt.Sprintf("Step %d (pause escalation until team's working hours) waits for the business hours of a team,\n"+
			"but notify_to_team_members is empty.", escalationStepPauseUntilTeamWorkingHours),
		ExitCode: &exitCode,
		Suggestions: []string{
			"Set notify_to_team_members to the ID of a team",
			"List the teams and their IDs with: gcx irm oncall teams list",
		},
	}
}

// checkEscalationPolicyTeam confirms that the team of a pause step exists, so
// that a mistyped ID gets a plain message instead of the backend's raw 400. It
// asks for that one team and not for the whole list. Only a definite "no such
// team" fails the check: when the lookup itself fails (permissions, network),
// the policy goes to the backend, which decides.
func checkEscalationPolicyTeam(ctx context.Context, c OnCallAPI, p *EscalationPolicy) error {
	step, ok := escalationStepValue(p.Step)
	if !ok || step != escalationStepPauseUntilTeamWorkingHours || teamID(p) == "" {
		return nil
	}

	_, err := c.GetTeam(ctx, teamID(p))
	var missing *resourceNotFoundError
	if !errors.As(err, &missing) {
		return nil
	}

	exitCode := gcxerrors.ExitUsageError
	return &gcxerrors.DetailedError{
		Summary:  fmt.Sprintf("Team %q not found", teamID(p)),
		Details:  "notify_to_team_members must be the ID of an existing team.",
		ExitCode: &exitCode,
		Suggestions: []string{
			"List the teams and their IDs with: gcx irm oncall teams list",
		},
	}
}

// teamID returns the trimmed team of a policy, or "" when it has none.
func teamID(p *EscalationPolicy) string {
	team, _ := p.NotifyToTeamMembers.(string)
	return strings.TrimSpace(team)
}

// escalationStepValue reads the numeric value of a step field, which decodes
// as a number from JSON or YAML and may also be written as a numeric string.
func escalationStepValue(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), v == float64(int(v))
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	default:
		return 0, false
	}
}
