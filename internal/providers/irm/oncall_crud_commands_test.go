//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
)

// fakeCRUDAPI stubs the route CRUD surface used by the verb-command tests.
// Unimplemented OnCallAPI methods panic via the embedded nil interface.
type fakeCRUDAPI struct {
	OnCallAPI

	createRouteFn func(context.Context, Route) (*Route, error)
	updateRouteFn func(context.Context, string, Route) (*Route, error)
	deletedIDs    []string
}

func (f *fakeCRUDAPI) CreateRoute(ctx context.Context, r Route) (*Route, error) {
	return f.createRouteFn(ctx, r)
}

func (f *fakeCRUDAPI) UpdateRoute(ctx context.Context, id string, r Route) (*Route, error) {
	return f.updateRouteFn(ctx, id, r)
}

func (f *fakeCRUDAPI) DeleteRoute(_ context.Context, id string) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runRoutesCmd(t *testing.T, fake *fakeCRUDAPI, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newRoutesCmd(&fakeLoader{client: fake})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestRouteCreateCommand(t *testing.T) {
	resetAgentMode(t)

	var gotBody Route
	fake := &fakeCRUDAPI{
		createRouteFn: func(_ context.Context, r Route) (*Route, error) {
			gotBody = r
			r.ID = "RNEW"
			return &r, nil
		},
	}
	manifest := writeManifest(t, `{"alert_receive_channel":"C1","filtering_term":".*","filtering_term_type":0}`)

	out, err := runRoutesCmd(t, fake, "", "create", "-f", manifest, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}

	if gotBody.AlertReceiveChannel != "C1" || gotBody.FilteringTerm != ".*" {
		t.Errorf("unexpected create body: %+v", gotBody)
	}

	var result Route
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if result.ID != "RNEW" {
		t.Errorf("expected created ID in output, got %+v", result)
	}
}

func TestRouteCreateCommandRequiresFilename(t *testing.T) {
	resetAgentMode(t)

	_, err := runRoutesCmd(t, &fakeCRUDAPI{}, "", "create")
	if err == nil || !strings.Contains(err.Error(), "--filename is required") {
		t.Errorf("expected missing-filename error, got %v", err)
	}
}

func TestRouteUpdateCommand(t *testing.T) {
	resetAgentMode(t)

	var gotID string
	fake := &fakeCRUDAPI{
		updateRouteFn: func(_ context.Context, id string, r Route) (*Route, error) {
			gotID = id
			r.ID = id
			return &r, nil
		},
	}
	manifest := writeManifest(t, "alert_receive_channel: C1\nfiltering_term: severity=critical\n")

	out, err := runRoutesCmd(t, fake, "", "update", "R42", "-f", manifest, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if gotID != "R42" {
		t.Errorf("expected update of R42, got %q", gotID)
	}

	var result Route
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if result.FilteringTerm != "severity=critical" {
		t.Errorf("unexpected updated route: %+v", result)
	}
}

func TestRouteDeleteCommandForce(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeCRUDAPI{}
	out, err := runRoutesCmd(t, fake, "", "delete", "R42", "--force")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.deletedIDs) != 1 || fake.deletedIDs[0] != "R42" {
		t.Errorf("expected delete of R42, got %v", fake.deletedIDs)
	}
	if !strings.Contains(out, "Deleted route R42") {
		t.Errorf("expected success message, got %q", out)
	}
}

func TestRouteDeleteCommandAborted(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeCRUDAPI{}
	_, err := runRoutesCmd(t, fake, "n\n", "delete", "R42")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.deletedIDs) != 0 {
		t.Errorf("expected no deletion after abort, got %v", fake.deletedIDs)
	}
}

func TestRouteDeleteCommandAgentModeRequiresForce(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	fake := &fakeCRUDAPI{}
	_, err := runRoutesCmd(t, fake, "", "delete", "R42")
	if !errors.Is(err, providers.ErrAgentModeRequiresForce) {
		t.Errorf("expected ErrAgentModeRequiresForce, got %v", err)
	}
	if len(fake.deletedIDs) != 0 {
		t.Errorf("expected no deletion in agent mode without --force, got %v", fake.deletedIDs)
	}
}

// fakeManifestAPI stubs the schedule and shift create surface used by the
// manifest-shape tests.
type fakeManifestAPI struct {
	OnCallAPI

	gotSchedule Schedule
	gotShift    ShiftRequest
}

func (f *fakeManifestAPI) CreateSchedule(_ context.Context, s Schedule) (*Schedule, error) {
	f.gotSchedule = s
	s.ID = "SNEW"
	return &s, nil
}

func (f *fakeManifestAPI) CreateShift(_ context.Context, s ShiftRequest) (*Shift, error) {
	f.gotShift = s
	return &Shift{ID: "OSNEW", Name: s.Name, Type: s.Type}, nil
}

// TestScheduleCreateAcceptsEnvelopeManifest pins issue #1185 problem 1: the
// K8s envelope that `resources list-examples` prints must reach the API with
// every spec field intact, exactly as the equivalent bare manifest does.
func TestScheduleCreateAcceptsEnvelopeManifest(t *testing.T) {
	resetAgentMode(t)

	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "envelope",
			manifest: `apiVersion: oncall.ext.grafana.app/v1alpha1
kind: Schedule
metadata:
  name: probe
spec:
  name: my schedule
  type: 2
  time_zone: Europe/Amsterdam
`,
		},
		{
			name: "bare spec",
			manifest: `name: my schedule
type: 2
time_zone: Europe/Amsterdam
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeManifestAPI{}
			cmd := newSchedulesCmd(&fakeLoader{client: fake})
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs([]string{"create", "-f", writeManifest(t, tt.manifest), "-o", "json"})
			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}

			got := fake.gotSchedule
			if got.Name != "my schedule" {
				t.Errorf("name = %q, want %q", got.Name, "my schedule")
			}
			if got.Type != float64(2) {
				t.Errorf("type = %#v, want float64(2)", got.Type)
			}
			if got.TimeZone != "Europe/Amsterdam" {
				t.Errorf("time_zone = %q, want %q", got.TimeZone, "Europe/Amsterdam")
			}
		})
	}
}

// TestShiftCreateAcceptsEnvelopeManifest covers the rotation fields that issue
// #1185 reports as unreachable from a manifest.
func TestShiftCreateAcceptsEnvelopeManifest(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeManifestAPI{}
	cmd := newShiftsCmd(&fakeLoader{client: fake})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"create", "-f", writeManifest(t, `apiVersion: oncall.ext.grafana.app/v1alpha1
kind: Shift
metadata:
  name: my-shift
spec:
  name: my-shift
  type: 2
  schedule: SBBBBBBBBBBBB
  shift_start: "2026-01-01T00:00:00"
  frequency: 1
  interval: 1
  by_day: [MO, TU]
  rolling_users: [[U1], [U2]]
`), "-o", "json"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := fake.gotShift
	if got.Name != "my-shift" {
		t.Errorf("name = %q, want %q", got.Name, "my-shift")
	}
	if got.Type != float64(2) {
		t.Errorf("type = %#v, want float64(2)", got.Type)
	}
	if got.Frequency != float64(1) {
		t.Errorf("frequency = %#v, want float64(1)", got.Frequency)
	}
	if got.RollingUsers == nil {
		t.Error("rolling_users = nil, want the manifest value")
	}
	if len(got.ByDay) != 2 {
		t.Errorf("by_day = %v, want two entries", got.ByDay)
	}
}
