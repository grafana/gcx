package alert

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &AlertProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AlertProvider{})
}

// AlertProvider manages Grafana alerting resources.
type AlertProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AlertProvider) Name() string { return "alert" }

// ShortDesc returns a one-line description of the provider.
func (p *AlertProvider) ShortDesc() string { return "Manage Grafana alert rules and alert groups" }

// Commands returns the Cobra commands contributed by this provider.
func (p *AlertProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	alertCmd := &cobra.Command{
		Use:   "alert",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(alertCmd.PersistentFlags())

	alertCmd.AddCommand(rulesCommands(loader))
	alertCmd.AddCommand(groupsCommands(loader))
	alertCmd.AddCommand(instancesCommands(loader))
	alertCmd.AddCommand(contactPointsCommands(loader))
	alertCmd.AddCommand(muteTimingsCommands(loader))
	alertCmd.AddCommand(notificationPoliciesCommands(loader))
	alertCmd.AddCommand(templatesCommands(loader))

	return []*cobra.Command{alertCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *AlertProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *AlertProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for Alert resource types.
// Registrations are added globally by providers.Register() which calls this method.
func (p *AlertProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:     NewRulesAdapterFactory(loader),
			Descriptor:  staticRulesDescriptor,
			GVK:         staticRulesDescriptor.GroupVersionKind(),
			Schema:      alertRuleSchema(),
			URLTemplate: "/alerting/{name}/view",
		},
		{
			Factory:    NewGroupsAdapterFactory(loader),
			Descriptor: staticGroupsDescriptor,
			GVK:        staticGroupsDescriptor.GroupVersionKind(),
			Schema:     alertRuleGroupSchema(),
		},
	}
}
