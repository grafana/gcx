package irm

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
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

	return []*cobra.Command{irmCmd}
}

func (p *IRMProvider) Validate(_ map[string]string) error        { return nil }
func (p *IRMProvider) ConfigKeys() []providers.ConfigKey          { return nil }
func (p *IRMProvider) TypedRegistrations() []adapter.Registration {
	loader := &configLoader{}
	regs := buildOnCallRegistrations(loader)
	regs = append(regs, buildIncidentRegistrations(loader)...)
	return regs
}
