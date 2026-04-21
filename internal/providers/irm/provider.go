package irm

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &IRMProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&IRMProvider{})
}

// IRMProvider manages Grafana IRM resources (OnCall + Incidents).
type IRMProvider struct{}

func (p *IRMProvider) Name() string      { return "irm" }
func (p *IRMProvider) ShortDesc() string { return "Manage Grafana IRM (OnCall + Incidents)" }

// ProductName implements framework.StatusDetectable.
func (p *IRMProvider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *IRMProvider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	// TODO: add proper error handling once provider setup is implemented
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *IRMProvider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *IRMProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *IRMProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *IRMProvider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

func (p *IRMProvider) Commands() []*cobra.Command {
	loader := &configLoader{}

	irmCmd := &cobra.Command{
		Use:   "irm",
		Short: p.ShortDesc(),
	}

	oncallCmd := &cobra.Command{
		Use:     "oncall",
		Short:   "Manage Grafana OnCall resources.",
		Aliases: []string{"oc"},
	}

	loader.BindFlags(irmCmd.PersistentFlags())

	oncallCmd.AddCommand(
		newIntegrationsCmd(loader),
		newEscalationChainsCmd(loader),
		newEscalationPoliciesCmd(loader),
		newSchedulesCmd(loader),
		newShiftsCmd(loader),
		newRoutesCmd(loader),
		newWebhooksCmd(loader),
		newAlertGroupsCommand(loader),
		newUsersCommand(loader),
		newTeamsCmd(loader),
		newUserGroupsCmd(loader),
		newSlackChannelsCmd(loader),
		newAlertsCmd(loader),
		newOrganizationsCmd(loader),
		newResolutionNotesCmd(loader),
		newShiftSwapsCmd(loader),
		newEscalateCommand(loader),
	)

	irmCmd.AddCommand(oncallCmd)
	irmCmd.AddCommand(newIncidentsCmd(loader))
	irmCmd.AddCommand(newSetupCommand(p))

	return []*cobra.Command{irmCmd}
}

func (p *IRMProvider) Validate(_ map[string]string) error { return nil }
func (p *IRMProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "oncall-url"},
	}
}
func (p *IRMProvider) TypedRegistrations() []adapter.Registration {
	loader := &configLoader{}
	regs := buildOnCallRegistrations(loader)
	regs = append(regs, buildIncidentRegistrations(loader)...)
	return regs
}
