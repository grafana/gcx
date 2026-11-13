//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTeamAPI stubs the team working-hours surface. Unimplemented OnCallAPI
// methods panic via the embedded nil interface.
type fakeTeamAPI struct {
	OnCallAPI

	hours    map[string]any
	gotID    string
	gotHours map[string]any
}

func (f *fakeTeamAPI) GetTeamWorkingHours(_ context.Context, id string) (map[string]any, error) {
	f.gotID = id
	return f.hours, nil
}

func (f *fakeTeamAPI) UpdateTeamWorkingHours(_ context.Context, id string, h map[string]any) (map[string]any, error) {
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
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestTeamGetWorkingHoursCommand(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeTeamAPI{hours: map[string]any{
		"irm_working_hours": []any{map[string]any{"name": "Office hours"}},
	}}

	out, err := runTeamsCmd(t, fake, "get-working-hours", "T1", "-o", "json")
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
	if intervals, _ := got["irm_working_hours"].([]any); len(intervals) != 1 {
		t.Errorf("unexpected intervals: %v", got["irm_working_hours"])
	}
}

func TestTeamUpdateWorkingHoursCommand(t *testing.T) {
	resetAgentMode(t)

	file := filepath.Join(t.TempDir(), "hours.yaml")
	doc := `irm_working_hours:
  - name: Office hours
    irm_working_hours:
      monday: [{start: "09:00:00", end: "17:00:00"}]
      sunday: []
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTeamAPI{}
	if _, err := runTeamsCmd(t, fake, "update-working-hours", "T1", "-f", file, "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if fake.gotID != "T1" {
		t.Errorf("expected update of T1, got %q", fake.gotID)
	}

	intervals, _ := fake.gotHours["irm_working_hours"].([]any)
	if len(intervals) != 1 {
		t.Fatalf("expected one interval, got %v", fake.gotHours)
	}
	interval, _ := intervals[0].(map[string]any)
	if interval["name"] != "Office hours" {
		t.Errorf("unexpected interval: %v", interval)
	}
}

func TestTeamUpdateWorkingHoursClear(t *testing.T) {
	resetAgentMode(t)

	file := filepath.Join(t.TempDir(), "clear.yaml")
	if err := os.WriteFile(file, []byte("irm_working_hours: null\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTeamAPI{}
	if _, err := runTeamsCmd(t, fake, "update-working-hours", "T1", "-f", file, "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if v, ok := fake.gotHours["irm_working_hours"]; !ok || v != nil {
		t.Errorf("expected an explicit null to reach the backend, got %v", fake.gotHours)
	}
}

func TestTeamUpdateWorkingHoursRejectsInvalidBeforeCallingBackend(t *testing.T) {
	resetAgentMode(t)

	file := filepath.Join(t.TempDir(), "bad.yaml")
	doc := `irm_working_hours:
  - name: Office hours
    irm_working_hours:
      monday: [{start: "17:00:00", end: "09:00:00"}]
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTeamAPI{}
	_, err := runTeamsCmd(t, fake, "update-working-hours", "T1", "-f", file)
	if err == nil || !strings.Contains(err.Error(), "must be before end") {
		t.Fatalf("expected a validation error, got %v", err)
	}
	if fake.gotID != "" {
		t.Errorf("the backend must not be called for an invalid document, got a call for %q", fake.gotID)
	}
}

func TestTeamGetWorkingHoursTable(t *testing.T) {
	resetAgentMode(t)

	week := func(monday ...any) map[string]any {
		return map[string]any{"monday": monday, "tuesday": []any{}}
	}
	rng := func(start, end string) any { return map[string]any{"start": start, "end": end} }

	fake := &fakeTeamAPI{hours: map[string]any{"irm_working_hours": []any{
		map[string]any{"name": "Business hours", "irm_working_hours": week(rng("09:00:00", "12:00:00"), rng("13:00:00", "17:30:15"))},
	}}}

	// The grid is the default format.
	out, err := runTeamsCmd(t, fake, "get-working-hours", "T1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INTERVAL", "MON", "SUN", "Business hours", "09:00-12:00, 13:00-17:30:15"} {
		if !strings.Contains(out, want) {
			t.Errorf("table is missing %q:\n%s", want, out)
		}
	}
}

func TestTeamGetWorkingHoursTableEmpty(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeTeamAPI{hours: map[string]any{"irm_working_hours": nil}}
	out, err := runTeamsCmd(t, fake, "get-working-hours", "T1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No business hours are set for this team.") {
		t.Errorf("expected the empty message, got:\n%s", out)
	}

	// The document formats still emit the document itself, including the null.
	out, err = runTeamsCmd(t, fake, "get-working-hours", "T1", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if v, ok := got["irm_working_hours"]; !ok || v != nil {
		t.Errorf("expected an explicit null document, got %v", got)
	}
}

func TestTeamUpdateWorkingHoursPrintsGridByDefault(t *testing.T) {
	resetAgentMode(t)

	file := filepath.Join(t.TempDir(), "hours.yaml")
	doc := `irm_working_hours:
  - name: Office hours
    irm_working_hours:
      monday: [{start: "09:00:00", end: "17:00:00"}]
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runTeamsCmd(t, &fakeTeamAPI{}, "update-working-hours", "T1", "-f", file)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INTERVAL", "MON", "Office hours", "09:00-17:00"} {
		if !strings.Contains(out, want) {
			t.Errorf("grid is missing %q:\n%s", want, out)
		}
	}

	// The document formats still emit the stored document.
	out, err = runTeamsCmd(t, &fakeTeamAPI{}, "update-working-hours", "T1", "-f", file, "-o", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "irm_working_hours:") || strings.Contains(out, "INTERVAL") {
		t.Errorf("expected the YAML document, got:\n%s", out)
	}
}
