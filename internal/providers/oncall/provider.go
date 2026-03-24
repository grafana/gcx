package oncall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var _ providers.Provider = &OnCallProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&OnCallProvider{})
	RegisterAdapters(&configLoader{})
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
		newPersonalNotificationRulesCmd(loader),
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

// ResourceAdapters returns adapter factories for OnCall resource types.
// Factories are registered globally via adapter.Register() in resource_adapter.go init().
func (p *OnCallProvider) ResourceAdapters() []adapter.Factory {
	return nil
}

// OnCallConfigLoader can produce a configured OnCall client.
type OnCallConfigLoader interface {
	LoadOnCallClient(ctx context.Context) (*Client, string, error)
}

// configLoader loads OnCall config and creates the client.
type configLoader struct {
	configFile string
	ctxName    string
}

func (l *configLoader) bindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&l.configFile, "config", "", "Path to the configuration file to use")
	flags.StringVar(&l.ctxName, "context", "", "Name of the context to use")
}

// LoadOnCallClient loads config, discovers the OnCall API URL, and returns a configured client.
func (l *configLoader) LoadOnCallClient(ctx context.Context) (*Client, string, error) {
	restCfg, err := l.LoadRESTConfig(ctx)
	if err != nil {
		return nil, "", err
	}

	// Check for explicit OnCall URL from env or provider config.
	oncallURL := os.Getenv("GRAFANA_ONCALL_URL")
	if oncallURL == "" {
		oncallURL = l.oncallURLFromConfig(ctx)
	}

	// Discover from plugin settings if not explicitly configured.
	if oncallURL == "" {
		discovered, discoverErr := DiscoverOnCallURL(ctx, restCfg)
		if discoverErr != nil {
			return nil, "", fmt.Errorf("failed to discover OnCall API URL: %w", discoverErr)
		}
		oncallURL = discovered
	}

	client := NewClient(oncallURL, restCfg.Host, restCfg.BearerToken)
	return client, restCfg.Namespace, nil
}

// oncallURLFromConfig attempts to read the OnCall URL from the provider config.
func (l *configLoader) oncallURLFromConfig(ctx context.Context) string {
	loaded, err := l.loadConfig(ctx)
	if err != nil {
		return ""
	}
	curCtx := loaded.GetCurrentContext()
	if curCtx == nil {
		return ""
	}
	if prov := curCtx.Providers["oncall"]; prov != nil {
		return prov["oncall-url"]
	}
	return ""
}

// LoadRESTConfig loads the REST config from the config file.
func (l *configLoader) LoadRESTConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
	loaded, err := l.loadConfig(ctx)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}

	if !loaded.HasContext(loaded.CurrentContext) {
		return config.NamespacedRESTConfig{}, fmt.Errorf("context %q not found", loaded.CurrentContext)
	}

	return loaded.GetCurrentContext().ToRESTConfig(ctx), nil
}

func (l *configLoader) loadConfig(ctx context.Context) (config.Config, error) {
	source := l.configSource()

	overrides := []config.Override{
		func(cfg *config.Config) error {
			if cfg.CurrentContext == "" {
				cfg.CurrentContext = config.DefaultContextName
			}

			if !cfg.HasContext(cfg.CurrentContext) {
				cfg.SetContext(cfg.CurrentContext, true, config.Context{})
			}

			curCtx := cfg.Contexts[cfg.CurrentContext]
			if curCtx.Grafana == nil {
				curCtx.Grafana = &config.GrafanaConfig{}
			}

			if err := env.Parse(curCtx); err != nil {
				return err
			}

			// Resolve GRAFANA_PROVIDER_{NAME}_{KEY} environment variables.
			const providerEnvPrefix = "GRAFANA_PROVIDER_"
			for _, envVar := range os.Environ() {
				parts := strings.SplitN(envVar, "=", 2)
				if len(parts) != 2 {
					continue
				}

				key, val := parts[0], parts[1]
				if !strings.HasPrefix(key, providerEnvPrefix) {
					continue
				}

				suffix := key[len(providerEnvPrefix):]
				nameParts := strings.SplitN(suffix, "_", 2)
				if len(nameParts) != 2 || nameParts[0] == "" || nameParts[1] == "" {
					continue
				}

				providerName := strings.ToLower(nameParts[0])
				configKey := strings.ReplaceAll(strings.ToLower(nameParts[1]), "_", "-")

				if curCtx.Providers == nil {
					curCtx.Providers = make(map[string]map[string]string)
				}
				if curCtx.Providers[providerName] == nil {
					curCtx.Providers[providerName] = make(map[string]string)
				}
				curCtx.Providers[providerName][configKey] = val
			}

			return nil
		},
	}

	ctxName := l.ctxName
	if ctxName == "" {
		ctxName = config.ContextNameFromCtx(ctx)
	}
	if ctxName != "" {
		overrides = append(overrides, func(cfg *config.Config) error {
			if !cfg.HasContext(ctxName) {
				return config.ContextNotFound(ctxName)
			}
			cfg.CurrentContext = ctxName
			return nil
		})
	}

	overrides = append(overrides, func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		curCtx := cfg.GetCurrentContext()
		if curCtx == nil {
			return errors.New("current context is nil")
		}
		return curCtx.Validate()
	})

	return config.Load(ctx, source, overrides...)
}

func (l *configLoader) configSource() config.Source {
	if l.configFile != "" {
		return config.ExplicitConfigFile(l.configFile)
	}
	return config.StandardLocation()
}
