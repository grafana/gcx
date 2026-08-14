//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeDiscoveryAPI stubs the four enum catalogs. Unimplemented OnCallAPI
// methods panic via the embedded nil interface.
type fakeDiscoveryAPI struct {
	OnCallAPI
}

func (f *fakeDiscoveryAPI) ListEscalationStepOptions(context.Context) ([]EscalationStepOption, error) {
	return []EscalationStepOption{{Value: 19, DisplayName: "Declare incident"}}, nil
}

func (f *fakeDiscoveryAPI) ListRouteFilterTypes(context.Context) ([]RouteFilterType, error) {
	return []RouteFilterType{{Value: 0, DisplayName: "Regular expression"}}, nil
}

func (f *fakeDiscoveryAPI) ListWebhookTriggerOptions(context.Context) ([]WebhookTriggerOption, error) {
	return []WebhookTriggerOption{{Value: 12, DisplayName: "Incident changed"}}, nil
}

func (f *fakeDiscoveryAPI) ListWebhookPresets(context.Context) ([]WebhookPreset, error) {
	return []WebhookPreset{{ID: "grafana_assistant", Name: "Grafana Assistant"}}, nil
}

func runDiscoveryCmd(t *testing.T, build func(OnCallConfigLoader) *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd := build(&fakeLoader{client: &fakeDiscoveryAPI{}})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// TestDiscoveryGroupListsByDefault is the regression test for the reported
// defect: the parent name reads as a command that discovers something, but it
// printed help and demanded a further `list`.
func TestDiscoveryGroupListsByDefault(t *testing.T) {
	tests := []struct {
		name  string
		build func(OnCallConfigLoader) *cobra.Command
		want  string
	}{
		{name: "escalation policy steps", build: newEscalationStepsCmd, want: "Declare incident"},
		{name: "route filter types", build: newRouteFilterTypesCmd, want: "Regular expression"},
		{name: "webhook triggers", build: newWebhookTriggersCmd, want: "Incident changed"},
		{name: "webhook presets", build: newWebhookPresetsCmd, want: "grafana_assistant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)

			bare, err := runDiscoveryCmd(t, tt.build)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(bare, tt.want) {
				t.Errorf("the bare command did not list the catalog, got %q", bare)
			}

			// The `list` child shipped, so it must keep working.
			withList, err := runDiscoveryCmd(t, tt.build, "list")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(withList, tt.want) {
				t.Errorf("`list` did not list the catalog, got %q", withList)
			}
		})
	}
}

// TestDiscoveryGroupFlagsAreIndependent guards the two flag sets: the parent
// and the child each carry their own options, so a format chosen on one never
// leaks into the other.
func TestDiscoveryGroupFlagsAreIndependent(t *testing.T) {
	resetAgentMode(t)

	out, err := runDiscoveryCmd(t, newEscalationStepsCmd, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("expected JSON from the bare command, got %q", out)
	}

	table, err := runDiscoveryCmd(t, newEscalationStepsCmd, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table, "DISPLAY NAME") {
		t.Errorf("expected the table from `list`, got %q", table)
	}
}

func TestDiscoveryGroupRejectsPositionalArgs(t *testing.T) {
	resetAgentMode(t)

	_, err := runDiscoveryCmd(t, newEscalationStepsCmd, "unexpected")
	if err == nil {
		t.Error("expected an error for an unknown positional argument")
	}
}
