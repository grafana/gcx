//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"sigs.k8s.io/yaml"
)

// fakeTeamAPI stubs the team-hours surface. Unimplemented OnCallAPI
// methods panic via the embedded nil interface.
type fakeTeamAPI struct {
	OnCallAPI

	hours    map[string]any
	gotID    string
	gotHours map[string]any
}

func (f *fakeTeamAPI) GetTeamHours(_ context.Context, id string) (map[string]any, error) {
	f.gotID = id
	return f.hours, nil
}

func (f *fakeTeamAPI) UpdateTeamHours(_ context.Context, id string, h map[string]any) (map[string]any, error) {
	f.gotID = id
	f.gotHours = h
	return h, nil
}

func runTeamsCmd(t *testing.T, fake *fakeTeamAPI, args ...string) (string, error) {
	t.Helper()
	cmd := newTeamsCmd(&fakeLoader{client: fake})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestTeamGetHoursCommand(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeTeamAPI{hours: map[string]any{
		"team_hours": []any{map[string]any{"name": "Office hours"}},
	}}

	out, err := runTeamsCmd(t, fake, "get-team-hours", "T1", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if fake.gotID != "T1" {
		t.Errorf("expected hours of T1, got %q", fake.gotID)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if intervals, _ := got["team_hours"].([]any); len(intervals) != 1 {
		t.Errorf("unexpected intervals: %v", got["team_hours"])
	}
}

func TestTeamUpdateHoursCommand(t *testing.T) {
	resetAgentMode(t)

	file := filepath.Join(t.TempDir(), "hours.yaml")
	doc := `team_hours:
  - name: Office hours
    hours:
      monday: [{start: "09:00:00", end: "17:00:00"}]
      sunday: []
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTeamAPI{}
	if _, err := runTeamsCmd(t, fake, "update-team-hours", "T1", "-f", file, "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if fake.gotID != "T1" {
		t.Errorf("expected update of T1, got %q", fake.gotID)
	}

	intervals, _ := fake.gotHours["team_hours"].([]any)
	if len(intervals) != 1 {
		t.Fatalf("expected one interval, got %v", fake.gotHours)
	}
	interval, _ := intervals[0].(map[string]any)
	if interval["name"] != "Office hours" {
		t.Errorf("unexpected interval: %v", interval)
	}
}

func TestTeamUpdateHoursClear(t *testing.T) {
	for _, tc := range []struct {
		name, value, answer string
		force, agentMode    bool
		autoApprove         bool
		wantPrompt          bool
	}{
		{name: "null declined", value: "null", answer: "n\n", wantPrompt: true},
		{name: "empty list confirmed", value: "[]", answer: "y\n", wantPrompt: true},
		{name: "null forced", value: "null", force: true},
		{name: "empty list blocked in agent mode", value: "[]", agentMode: true},
		{name: "empty list auto-approved in agent mode", value: "[]", agentMode: true, autoApprove: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetAgentMode(t, tc.agentMode)
			t.Setenv("GCX_AUTO_APPROVE", strconv.FormatBool(tc.autoApprove))

			file := filepath.Join(t.TempDir(), "clear.yaml")
			if err := os.WriteFile(file, []byte("team_hours: "+tc.value+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			fake := &fakeTeamAPI{}
			cmd := newTeamsCmd(&fakeLoader{client: fake})
			cmd.SilenceUsage = true
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(tc.answer))
			args := []string{"update-team-hours", "T1", "-f", file, "-o", "json"}
			if tc.force {
				args = append(args, "--force")
			}
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(context.Background())
			if got := strings.Contains(stderr.String(), `Clear all business hours for team "T1"? [y/N]`); got != tc.wantPrompt {
				t.Errorf("expected prompt: %v, stderr: %q", tc.wantPrompt, stderr.String())
			}

			switch {
			case tc.answer == "n\n":
				detail, ok := errors.AsType[*gcxerrors.DetailedError](err)
				if !ok || detail.ExitCode == nil || *detail.ExitCode != gcxerrors.ExitCancelled {
					t.Fatalf("expected a cancellation error, got %v", err)
				}
			case tc.agentMode && !tc.autoApprove:
				if !errors.Is(err, providers.ErrAgentModeRequiresForce) {
					t.Fatalf("expected --force error, got %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				want := `{"team_hours":` + tc.value + `}`
				got, err := json.Marshal(fake.gotHours)
				if err != nil || string(got) != want || fake.gotID != "T1" {
					t.Errorf("expected %s sent to T1, got %s sent to %q (error: %v)", want, got, fake.gotID, err)
				}
				return
			}
			if fake.gotID != "" || stdout.Len() != 0 {
				t.Errorf("expected no update or result, got a call for %q and stdout %q", fake.gotID, stdout.String())
			}
		})
	}
}

func TestTeamUpdateHoursRejectsInvalidBeforeCallingBackend(t *testing.T) {
	resetAgentMode(t)

	file := filepath.Join(t.TempDir(), "bad.yaml")
	doc := `team_hours:
  - name: Office hours
    hours:
      funday: [{start: "09:00:00", end: "17:00:00"}]
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTeamAPI{}
	_, err := runTeamsCmd(t, fake, "update-team-hours", "T1", "-f", file)
	if err == nil || !strings.Contains(err.Error(), `unknown weekday "funday"`) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	if fake.gotID != "" {
		t.Errorf("the backend must not be called for an invalid document, got a call for %q", fake.gotID)
	}
}

func TestTeamGetHoursTable(t *testing.T) {
	resetAgentMode(t)

	week := func(monday ...any) map[string]any {
		return map[string]any{"monday": monday, "tuesday": []any{}}
	}
	rng := func(start, end string) any { return map[string]any{"start": start, "end": end} }

	fake := &fakeTeamAPI{hours: map[string]any{"team_hours": []any{
		map[string]any{"name": "Business hours", "hours": week(rng("09:00:00", "12:00:00"), rng("13:00:00", "17:30:15"))},
	}}}

	// The grid is the default format.
	out, err := runTeamsCmd(t, fake, "get-team-hours", "T1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INTERVAL", "MON", "SUN", "Business hours", "09:00-12:00, 13:00-17:30:15"} {
		if !strings.Contains(out, want) {
			t.Errorf("table is missing %q:\n%s", want, out)
		}
	}
}

func TestTeamGetHoursTableEmpty(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeTeamAPI{hours: map[string]any{"team_hours": nil}}
	out, err := runTeamsCmd(t, fake, "get-team-hours", "T1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No business hours are set for this team.") {
		t.Errorf("expected the empty message, got:\n%s", out)
	}

	// The document formats still emit the document itself, including the null.
	out, err = runTeamsCmd(t, fake, "get-team-hours", "T1", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if v, ok := got["team_hours"]; !ok || v != nil {
		t.Errorf("expected an explicit null document, got %v", got)
	}
}

func TestTeamUpdateHoursOutputFormats(t *testing.T) {
	resetAgentMode(t)
	t.Setenv("GCX_AUTO_APPROVE", "false")

	file := filepath.Join(t.TempDir(), "hours.yaml")
	doc := `team_hours:
  - name: Office hours
    hours:
      monday: [{start: "09:00:00", end: "17:00:00"}]
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTeamAPI{}
	out, err := runTeamsCmd(t, fake, "update-team-hours", "T1", "-f", file)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out), &got); err != nil || !reflect.DeepEqual(got, fake.gotHours) || !strings.HasPrefix(out, "team_hours:\n") {
		t.Errorf("expected the stored document as YAML, got:\n%s", out)
	}

	out, err = runTeamsCmd(t, fake, "update-team-hours", "T1", "-f", file, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INTERVAL", "MON", "Office hours", "09:00-17:00"} {
		if !strings.Contains(out, want) {
			t.Errorf("grid is missing %q:\n%s", want, out)
		}
	}
}
