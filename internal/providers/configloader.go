package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/grafana/grafanactl/internal/cloud"
	"github.com/grafana/grafanactl/internal/config"
	"github.com/spf13/pflag"
)

// CloudRESTConfig holds the resolved Grafana Cloud configuration needed to
// authenticate against cloud platform APIs.
type CloudRESTConfig struct {
	Token     string
	Stack     cloud.StackInfo
	Namespace string
}

// ConfigLoader is a minimal config loading helper shared across providers.
// It avoids importing cmd/grafanactl/config (which would create an import cycle
// via internal/providers).
type ConfigLoader struct {
	configFile string
	ctxName    string
}

// BindFlags registers --config and --context flags on the given flag set.
func (l *ConfigLoader) BindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&l.configFile, "config", "", "Path to the configuration file to use")
	flags.StringVar(&l.ctxName, "context", "", "Name of the context to use")
}

// SetContextName sets the config context name to use when loading config.
// This is used by provider adapter factories to honour the --context flag
// threaded via context.Context.
func (l *ConfigLoader) SetContextName(name string) {
	l.ctxName = name
}

// SetConfigFile sets the path to the configuration file to use.
// This is intended for testing and programmatic use where flag parsing is not available.
func (l *ConfigLoader) SetConfigFile(path string) {
	l.configFile = path
}

// LoadGrafanaConfig loads the REST config from the config file, applying
// env var overrides and context flags. It mirrors the logic in
// cmd/grafanactl/config.Options.LoadGrafanaConfig.
func (l *ConfigLoader) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
	overrides := []config.Override{
		// Apply env vars into the current context.
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

	// Resolve context name: explicit flag takes priority, then context.Context carrier
	// (set by resource commands to honour the --context flag for provider adapters).
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

	// Validate after loading.
	overrides = append(overrides, func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		return cfg.GetCurrentContext().Validate()
	})

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}

	if !loaded.HasContext(loaded.CurrentContext) {
		return config.NamespacedRESTConfig{}, fmt.Errorf("context %q not found", loaded.CurrentContext)
	}

	return loaded.GetCurrentContext().ToRESTConfig(ctx), nil
}

// LoadCloudConfig loads Grafana Cloud configuration, applying env var overrides.
// Unlike LoadGrafanaConfig it does not require grafana.server to be set.
// It validates that cloud.token is present, resolves the stack slug and GCOM URL,
// calls the GCOM API to discover stack info, and returns a CloudRESTConfig.
func (l *ConfigLoader) LoadCloudConfig(ctx context.Context) (CloudRESTConfig, error) {
	overrides := []config.Override{
		// Apply env vars into the current context.
		func(cfg *config.Config) error {
			if cfg.CurrentContext == "" {
				cfg.CurrentContext = config.DefaultContextName
			}

			if !cfg.HasContext(cfg.CurrentContext) {
				cfg.SetContext(cfg.CurrentContext, true, config.Context{})
			}

			curCtx := cfg.Contexts[cfg.CurrentContext]
			if curCtx.Cloud == nil {
				curCtx.Cloud = &config.CloudConfig{}
			}

			if err := env.Parse(curCtx); err != nil {
				return err
			}

			return nil
		},
	}

	// Resolve context name.
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

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
	if err != nil {
		return CloudRESTConfig{}, err
	}

	if !loaded.HasContext(loaded.CurrentContext) {
		return CloudRESTConfig{}, fmt.Errorf("context %q not found", loaded.CurrentContext)
	}

	curCtx := loaded.GetCurrentContext()

	// Validate cloud token.
	if curCtx.Cloud == nil || curCtx.Cloud.Token == "" {
		return CloudRESTConfig{}, errors.New("cloud token is required: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var")
	}

	token := curCtx.Cloud.Token

	// Resolve stack slug.
	slug := curCtx.ResolveStackSlug()
	if slug == "" {
		return CloudRESTConfig{}, errors.New("cloud stack is not configured: set cloud.stack in config or GRAFANA_CLOUD_STACK env var")
	}

	// Resolve GCOM URL and fetch stack info.
	gcomURL := curCtx.ResolveGCOMURL()
	client := cloud.NewGCOMClient(gcomURL, token)

	stack, err := client.GetStack(ctx, slug)
	if err != nil {
		return CloudRESTConfig{}, fmt.Errorf("failed to get stack info for %q: %w", slug, err)
	}

	// Derive namespace from grafana config if available, else default.
	namespace := "default"
	if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
		namespace = curCtx.ToRESTConfig(ctx).Namespace
	}

	return CloudRESTConfig{
		Token:     token,
		Stack:     stack,
		Namespace: namespace,
	}, nil
}
