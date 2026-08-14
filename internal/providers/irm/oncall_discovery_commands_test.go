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

// newDiscoveryParent mounts one catalog pair under a parent command. The
// caller keeps the returned instance, so both invocations of the catalog run
// against the same command tree.
func newDiscoveryParent(t *testing.T, build func(OnCallConfigLoader) []*cobra.Command) *cobra.Command {
	t.Helper()
	parent := &cobra.Command{Use: "parent"}
	parent.AddCommand(build(&fakeLoader{client: &fakeDiscoveryAPI{}})...)
	return parent
}

// runDiscoveryCmd executes args against an existing command tree.
func runDiscoveryCmd(t *testing.T, parent *cobra.Command, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	parent.SetOut(out)
	parent.SetErr(out)
	parent.SetArgs(args)
	err := parent.ExecuteContext(context.Background())
	return out.String(), err
}

type discoveryCase struct {
	name     string
	build    func(OnCallConfigLoader) []*cobra.Command
	compound string
	noun     string
	want     string
}

// discoveryCases lists the four catalogs and the two spellings of each.
func discoveryCases() []discoveryCase {
	return []discoveryCase{
		{
			name:     "escalation policy steps",
			build:    newEscalationStepCmds,
			compound: "list-step-types",
			noun:     "steps",
			want:     "Declare incident",
		},
		{
			name:     "route filter types",
			build:    newRouteFilterTypeCmds,
			compound: "list-filter-types",
			noun:     "filter-types",
			want:     "Regular expression",
		},
		{
			name:     "webhook triggers",
			build:    newWebhookTriggerCmds,
			compound: "list-triggers",
			noun:     "triggers",
			want:     "Incident changed",
		},
		{
			name:     "webhook presets",
			build:    newWebhookPresetCmds,
			compound: "list-presets",
			noun:     "presets",
			want:     "grafana_assistant",
		},
	}
}

// TestDiscoveryCatalogHasBothInvocations checks the two spellings of each
// catalog: the canonical `list-<subject>` compound, and the older
// `<subject> list` noun group that shipped.
func TestDiscoveryCatalogHasBothInvocations(t *testing.T) {
	for _, tt := range discoveryCases() {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)
			parent := newDiscoveryParent(t, tt.build)

			compound, err := runDiscoveryCmd(t, parent, tt.compound)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(compound, tt.want) {
				t.Errorf("`%s` did not list the catalog, got %q", tt.compound, compound)
			}

			nounGroup, err := runDiscoveryCmd(t, parent, tt.noun, "list")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(nounGroup, tt.want) {
				t.Errorf("`%s list` did not list the catalog, got %q", tt.noun, nounGroup)
			}
		})
	}
}

// TestDiscoveryFlagsAreIndependent guards the two flag sets. Both invocations
// run against one command tree, so the test fails if the compound command and
// the `list` child share a single discoveryListOpts.
func TestDiscoveryFlagsAreIndependent(t *testing.T) {
	resetAgentMode(t)
	parent := newDiscoveryParent(t, newEscalationStepCmds)

	out, err := runDiscoveryCmd(t, parent, "list-step-types", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("expected JSON from `list-step-types`, got %q", out)
	}

	table, err := runDiscoveryCmd(t, parent, "steps", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table, "DISPLAY NAME") {
		t.Errorf("expected the table from `steps list`, got %q", table)
	}
}

// TestDiscoveryNounGroupIsNotRunnable keeps the noun group a group. A runnable
// bare noun leaf breaks the $AREA $NOUN $VERB grammar, and it also hides the
// enriched usage error that cmd/gcx/root/validation.go gives for a group.
func TestDiscoveryNounGroupIsNotRunnable(t *testing.T) {
	parent := newDiscoveryParent(t, newEscalationStepCmds)

	steps, _, err := parent.Find([]string{"steps"})
	if err != nil {
		t.Fatal(err)
	}
	if steps.Runnable() {
		t.Error("the `steps` noun group must not run on its own")
	}
}

// TestDiscoveryCompoundHasFullHelp holds the compound commands to the help
// text contract in docs/design/help-text.md: a Long of 2 sentences or more,
// and 3 examples or more.
func TestDiscoveryCompoundHasFullHelp(t *testing.T) {
	for _, tt := range discoveryCases() {
		t.Run(tt.name, func(t *testing.T) {
			parent := newDiscoveryParent(t, tt.build)
			cmd, _, err := parent.Find([]string{tt.compound})
			if err != nil {
				t.Fatal(err)
			}

			if got := strings.Count(cmd.Long, ". "); got < 1 {
				t.Errorf("`%s` needs a Long of 2 sentences or more, got %q", tt.compound, cmd.Long)
			}
			if got := strings.Count(cmd.Example, "#"); got < 3 {
				t.Errorf("`%s` needs 3 examples or more, got %q", tt.compound, cmd.Example)
			}
			if !strings.Contains(cmd.Example, tt.compound) {
				t.Errorf("the examples of `%s` must call the command, got %q", tt.compound, cmd.Example)
			}
		})
	}
}

// TestDiscoveryNounListNamesTheCompound keeps the canonicality signal in the
// human help. A person who reads the parent help must see which of the two
// spellings to use.
func TestDiscoveryNounListNamesTheCompound(t *testing.T) {
	for _, tt := range discoveryCases() {
		t.Run(tt.name, func(t *testing.T) {
			parent := newDiscoveryParent(t, tt.build)
			cmd, _, err := parent.Find([]string{tt.noun, "list"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(cmd.Short, tt.compound) {
				t.Errorf("the Short of `%s list` must name `%s`, got %q", tt.noun, tt.compound, cmd.Short)
			}
		})
	}
}

func TestDiscoveryRejectsPositionalArgs(t *testing.T) {
	resetAgentMode(t)
	parent := newDiscoveryParent(t, newEscalationStepCmds)

	if _, err := runDiscoveryCmd(t, parent, "list-step-types", "unexpected"); err == nil {
		t.Error("expected an error for an unknown positional argument")
	}
}
