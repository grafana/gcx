//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestValidateEscalationPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  EscalationPolicy
		wantErr bool
	}{
		{name: "pause with team", policy: EscalationPolicy{Step: 27, NotifyToTeamMembers: "T1"}},
		{name: "pause with team, step from YAML", policy: EscalationPolicy{Step: float64(27), NotifyToTeamMembers: "T1"}},
		{name: "pause with team, step as string", policy: EscalationPolicy{Step: "27", NotifyToTeamMembers: "T1"}},
		{name: "pause without team", policy: EscalationPolicy{Step: 27}, wantErr: true},
		{name: "pause with empty team", policy: EscalationPolicy{Step: 27, NotifyToTeamMembers: ""}, wantErr: true},
		{name: "pause with blank team", policy: EscalationPolicy{Step: 27, NotifyToTeamMembers: "  "}, wantErr: true},
		{name: "pause with non-string team", policy: EscalationPolicy{Step: float64(27), NotifyToTeamMembers: 5}, wantErr: true},
		{name: "pause without team, step from YAML", policy: EscalationPolicy{Step: float64(27)}, wantErr: true},
		{name: "pause without team, step as string", policy: EscalationPolicy{Step: " 27 "}, wantErr: true},
		{name: "other step without team", policy: EscalationPolicy{Step: 0}},
		{name: "notify team members step needs no check", policy: EscalationPolicy{Step: 17}},
		{name: "unknown step shape", policy: EscalationPolicy{Step: map[string]any{}}},
		{name: "no step", policy: EscalationPolicy{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateEscalationPolicy(&tt.policy)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected a valid policy, got: %v", err)
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
			if len(detailed.Suggestions) < 2 || !strings.Contains(detailed.Suggestions[1], "teams list") {
				t.Errorf("expected a suggestion to list teams, got %v", detailed.Suggestions)
			}
		})
	}
}

// fakePolicyAPI records the escalation policy calls. Unimplemented OnCallAPI
// methods panic via the embedded nil interface.
type fakePolicyAPI struct {
	OnCallAPI

	created []EscalationPolicy
	updated []EscalationPolicy

	// teams lists the team IDs that exist. teamErr, when set, makes every team
	// lookup fail with it.
	teams   []string
	teamErr error
	looked  []string
}

func (f *fakePolicyAPI) GetTeam(_ context.Context, id string) (*Team, error) {
	f.looked = append(f.looked, id)
	if f.teamErr != nil {
		return nil, f.teamErr
	}
	if slices.Contains(f.teams, id) {
		return &Team{ID: id}, nil
	}
	return nil, &resourceNotFoundError{resourceType: "team", id: id}
}

func (f *fakePolicyAPI) CreateEscalationPolicy(_ context.Context, p EscalationPolicy) (*EscalationPolicy, error) {
	f.created = append(f.created, p)
	return &p, nil
}

func (f *fakePolicyAPI) UpdateEscalationPolicy(_ context.Context, _ string, p EscalationPolicy) (*EscalationPolicy, error) {
	f.updated = append(f.updated, p)
	return &p, nil
}

func runPolicyCmd(t *testing.T, fake *fakePolicyAPI, stdin string, args ...string) error {
	t.Helper()
	cmd := newEscalationPoliciesCmd(&fakeLoader{client: fake})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs(args)
	return cmd.ExecuteContext(context.Background())
}

func TestEscalationPolicyCommandsRequireTeamForPauseStep(t *testing.T) {
	resetAgentMode(t)

	noTeam := "step: 27\nescalation_chain: C1\n"

	fake := &fakePolicyAPI{teams: []string{"T1"}}
	err := runPolicyCmd(t, fake, noTeam, "create", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "needs a team") {
		t.Fatalf("create: expected a missing-team error, got %v", err)
	}
	err = runPolicyCmd(t, fake, noTeam, "update", "P1", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "needs a team") {
		t.Fatalf("update: expected a missing-team error, got %v", err)
	}
	if len(fake.created)+len(fake.updated) != 0 {
		t.Errorf("the backend must not be called for an invalid policy: created=%v updated=%v", fake.created, fake.updated)
	}

	withTeam := "step: 27\nescalation_chain: C1\nnotify_to_team_members: T1\n"
	if err := runPolicyCmd(t, fake, withTeam, "create", "-f", "-"); err != nil {
		t.Fatalf("create with a team: %v", err)
	}
	if err := runPolicyCmd(t, fake, withTeam, "update", "P1", "-f", "-"); err != nil {
		t.Fatalf("update with a team: %v", err)
	}
	if len(fake.created) != 1 || fake.created[0].NotifyToTeamMembers != "T1" {
		t.Errorf("expected the team to reach the backend on create, got %v", fake.created)
	}
	if len(fake.updated) != 1 || fake.updated[0].NotifyToTeamMembers != "T1" {
		t.Errorf("expected the team to reach the backend on update, got %v", fake.updated)
	}
}

func TestEscalationPolicyCommandsRejectUnknownTeam(t *testing.T) {
	resetAgentMode(t)

	bogus := "step: 27\nescalation_chain: C1\nnotify_to_team_members: NOPE123\n"

	fake := &fakePolicyAPI{teams: []string{"T1"}}
	for _, args := range [][]string{{"create", "-f", "-"}, {"update", "P1", "-f", "-"}} {
		err := runPolicyCmd(t, fake, bogus, args...)
		var detailed *gcxerrors.DetailedError
		if !errors.As(err, &detailed) || detailed.Summary != `Team "NOPE123" not found` {
			t.Fatalf("%s: expected a team-not-found error, got %v", args[0], err)
		}
		if detailed.ExitCode == nil || *detailed.ExitCode != gcxerrors.ExitUsageError {
			t.Errorf("%s: expected the usage exit code, got %v", args[0], detailed.ExitCode)
		}
	}
	if len(fake.created)+len(fake.updated) != 0 {
		t.Errorf("the backend must not be called for an unknown team: created=%v updated=%v", fake.created, fake.updated)
	}
}

func TestEscalationPolicyTeamLookupFailureDoesNotBlock(t *testing.T) {
	resetAgentMode(t)

	// A lookup that fails for another reason, such as a permission error, says
	// nothing about the team, so the backend gets to decide.
	fake := &fakePolicyAPI{teamErr: errors.New("403 forbidden")}
	doc := "step: 27\nescalation_chain: C1\nnotify_to_team_members: T1\n"
	if err := runPolicyCmd(t, fake, doc, "create", "-f", "-"); err != nil {
		t.Fatalf("expected the policy to reach the backend, got %v", err)
	}
	if len(fake.created) != 1 {
		t.Errorf("expected one create call, got %d", len(fake.created))
	}
}

func TestEscalationPolicyTeamIsOnlyLookedUpForPauseStep(t *testing.T) {
	resetAgentMode(t)

	fake := &fakePolicyAPI{}
	doc := "step: 17\nescalation_chain: C1\nnotify_to_team_members: NOPE123\n"
	if err := runPolicyCmd(t, fake, doc, "create", "-f", "-"); err != nil {
		t.Fatalf("a notify-team step must not be checked: %v", err)
	}
	if len(fake.looked) != 0 {
		t.Errorf("expected no team lookup, got %v", fake.looked)
	}
}
