package vulnobs_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/vulnobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVulnobsProvider_Metadata(t *testing.T) {
	p := &vulnobs.VulnobsProvider{}

	assert.Equal(t, "vulnobs", p.Name())
	assert.NotEmpty(t, p.ShortDesc())

	regs := p.TypedRegistrations()
	require.Len(t, regs, 1, "Source should be the only typed registration; Issue is a sub-resource")

	r := regs[0]
	assert.Equal(t, vulnobs.SourceDescriptor().GroupVersionKind(), r.GVK)
	assert.NotEmpty(t, r.Schema, "Schema is required by CONSTITUTION line 42-47")
	assert.Nil(t, r.Example, "Example may be nil for read-only resources (CONSTITUTION line 45)")

	// No config keys; auth is inherited from the active Grafana context.
	assert.Nil(t, p.ConfigKeys())
	assert.NoError(t, p.Validate(nil))
}

func TestVulnobsProvider_Commands_Structure(t *testing.T) {
	p := &vulnobs.VulnobsProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1, "single top-level vulnobs command")

	root := cmds[0]
	assert.Equal(t, "vulnobs", root.Name())

	subNames := map[string]bool{}
	for _, sub := range root.Commands() {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["groups"], "must have groups subcommand")
	assert.True(t, subNames["projects"], "must have projects subcommand")
	assert.False(t, subNames["issues"], "Issue must NOT be a top-level command (CONSTITUTION line 130-135)")
}

func TestProjectsCommand_HasListIssuesSubcommand(t *testing.T) {
	p := &vulnobs.VulnobsProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	var projectsCmd, listIssuesCmd = (func() (string, string) {
		root := cmds[0]
		for _, sub := range root.Commands() {
			if sub.Name() != "projects" {
				continue
			}
			for _, leaf := range sub.Commands() {
				if leaf.Name() == "list-issues" {
					return sub.Name(), leaf.Name()
				}
			}
			return sub.Name(), ""
		}
		return "", ""
	})()
	assert.Equal(t, "projects", projectsCmd)
	assert.Equal(t, "list-issues", listIssuesCmd,
		"list-issues must nest under projects per CONSTITUTION line 130-135")
}

// fakeLoader returns a fixed config; used only to confirm the loader is
// implemented for AdapterFactory wiring. No HTTP calls are made.
type fakeLoader struct{ host string }

func (f fakeLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	cfg := config.NamespacedRESTConfig{}
	cfg.Host = f.host
	cfg.Namespace = "stack-x"
	return cfg, nil
}

func TestNewSourceAdapterFactory_BuildsAdapter(t *testing.T) {
	// Use a real ConfigLoader's interface shape via our fake; the factory only
	// reads cfg.Host/Namespace and constructs a TypedCRUD wrapper.
	factory := vulnobs.NewSourceAdapterFactory(fakeLoader{host: "http://example.test"})
	a, err := factory(context.Background())
	require.NoError(t, err)
	require.NotNil(t, a)
}

// Compile-time check that VulnobsProvider satisfies providers.Provider.
var _ providers.Provider = (*vulnobs.VulnobsProvider)(nil)
