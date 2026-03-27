package oncall

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var _ providers.Provider = &OnCallProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&OnCallProvider{})
}

// OnCallProvider manages Grafana OnCall resources.
type OnCallProvider struct{}

// Name returns the unique identifier for this provider.
func (p *OnCallProvider) Name() string { return "oncall" }

// ShortDesc returns a one-line description of the provider.
func (p *OnCallProvider) ShortDesc() string {
	return "Manage Grafana OnCall resources."
}

// Commands returns the Cobra commands contributed by this provider.
// Structure follows the canonical pattern: oncall <resource> <command>
// (e.g., oncall integrations list, oncall alert-groups get <id>).
func (p *OnCallProvider) Commands() []*cobra.Command {
	loader := &configLoader{}

	oncallCmd := &cobra.Command{
		Use:     "oncall",
		Short:   p.ShortDesc(),
		Aliases: []string{"oc"},
	}

	loader.bindFlags(oncallCmd.PersistentFlags())

	oncallCmd.AddCommand(
		// Resource groups: oncall <resource> list|get|...
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
		// personal-notification-rules removed: OnCall API rejects SA tokens
		// for this endpoint (403 "Invalid token"). Needs user-token auth support.
		// Standalone action commands
		newEscalateCommand(loader),
	)

	return []*cobra.Command{oncallCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *OnCallProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// The oncall provider discovers its URL from the IRM plugin settings
// and uses the standard Grafana SA token for authentication.
func (p *OnCallProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for OnCall resource types.
// Registrations are added globally by providers.Register() which calls this method.
func (p *OnCallProvider) TypedRegistrations() []adapter.Registration {
	return buildOnCallRegistrations(&configLoader{})
}

// OnCallConfigLoader can produce a configured OnCall client.
type OnCallConfigLoader interface {
	LoadOnCallClient(ctx context.Context) (*Client, string, error)
}

// configLoader loads OnCall config and creates the client.
// It composes providers.ConfigLoader for shared config loading logic
// and adds OnCall-specific URL discovery.
type configLoader struct {
	providers.ConfigLoader
}

func (l *configLoader) bindFlags(flags *pflag.FlagSet) {
	l.BindFlags(flags)
}

// LoadOnCallClient loads config, discovers the OnCall API URL, and returns a configured client.
func (l *configLoader) LoadOnCallClient(ctx context.Context) (*Client, string, error) {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", err
	}

	oncallURL, err := l.discoverOnCallURL(ctx, restCfg)
	if err != nil {
		return nil, "", err
	}

	client, err := NewClient(oncallURL, restCfg)
	if err != nil {
		return nil, "", err
	}
	return client, restCfg.Namespace, nil
}

// discoverOnCallURL resolves the OnCall API URL from provider config or plugin settings.
func (l *configLoader) discoverOnCallURL(ctx context.Context, restCfg config.NamespacedRESTConfig) (string, error) {
	// Check provider config (includes GRAFANA_PROVIDER_ONCALL_ONCALL_URL env var).
	providerCfg, _, err := l.LoadProviderConfig(ctx, "oncall")
	if err != nil {
		return "", err
	}
	if u := providerCfg["oncall-url"]; u != "" {
		return u, nil
	}

	// Discover from plugin settings.
	discovered, err := DiscoverOnCallURL(ctx, restCfg)
	if err != nil {
		return "", fmt.Errorf("failed to discover OnCall API URL: %w", err)
	}
	return discovered, nil
}
