package synth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/providers/synth/checks"
	"github.com/grafana/grafanactl/internal/providers/synth/probes"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&SynthProvider{})

	// Register static descriptors for checks and probes so that they appear in
	// the discovery registry and can be used as selectors without initializing
	// the provider config.
	loader := &configLoader{}
	adapter.Register(adapter.Registration{
		Factory:    checks.NewAdapterFactory(loader),
		Descriptor: checks.StaticDescriptor(),
		Aliases:    checks.StaticAliases(),
		GVK:        checks.StaticGVK(),
		Schema:     checkSchema(),
		Example:    checkExample(),
	})
	adapter.Register(adapter.Registration{
		Factory:    probes.NewAdapterFactory(loader),
		Descriptor: probes.StaticDescriptor(),
		Aliases:    probes.StaticAliases(),
		GVK:        probes.StaticGVK(),
		Schema:     probeSchema(),
	})
}

// checkSchema returns a JSON Schema for the SM Check resource type.
func checkSchema() json.RawMessage {
	return adapter.SchemaFromType[checks.CheckSpec](checks.StaticDescriptor())
}

// checkExample returns an example SM Check manifest as JSON.
func checkExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": checks.APIVersion,
		"kind":       checks.Kind,
		"metadata": map[string]any{
			"name": "web-check",
		},
		"spec": map[string]any{
			"job":              "web-check",
			"target":           "https://grafana.com",
			"frequency":        60000,
			"timeout":          5000,
			"enabled":          true,
			"probes":           []string{"Atlanta", "London", "Tokyo"},
			"settings":         map[string]any{"http": map[string]any{"method": "GET"}},
			"alertSensitivity": "medium",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("synth/checks: failed to marshal example: %v", err))
	}
	return b
}

// probeSchema returns a JSON Schema for the SM Probe resource type.
func probeSchema() json.RawMessage {
	return adapter.SchemaFromType[probes.Probe](probes.StaticDescriptor())
}

// SynthProvider manages Grafana Synthetic Monitoring resources.
type SynthProvider struct{}

// Name returns the unique identifier for this provider.
func (p *SynthProvider) Name() string { return "synth" }

// ShortDesc returns a one-line description of the provider.
func (p *SynthProvider) ShortDesc() string {
	return "Manage Grafana Synthetic Monitoring resources."
}

// Commands returns the Cobra commands contributed by this provider.
func (p *SynthProvider) Commands() []*cobra.Command {
	loader := &configLoader{}

	synthCmd := &cobra.Command{
		Use:   "synth",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
			if providers.IsCRUDCommand(cmd) {
				providers.WarnDeprecated(cmd, "grafanactl resources schemas checks")
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.bindFlags(synthCmd.PersistentFlags())

	synthCmd.AddCommand(checks.Commands(loader))
	synthCmd.AddCommand(probes.Commands(loader))

	return []*cobra.Command{synthCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *SynthProvider) Validate(cfg map[string]string) error {
	if cfg["sm-url"] == "" {
		return errors.New("sm-url is required for the synth provider")
	}
	if cfg["sm-token"] == "" {
		return errors.New("sm-token is required for the synth provider")
	}
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *SynthProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "sm-url", Secret: false},
		{Name: "sm-token", Secret: true},
		{Name: "sm-metrics-datasource-uid", Secret: false},
	}
}

// ResourceAdapters returns adapter factories for Synth resource types.
// Each factory uses a fresh configLoader to load SM credentials lazily on first invocation.
func (p *SynthProvider) ResourceAdapters() []adapter.Factory {
	loader := &configLoader{}
	return []adapter.Factory{
		checks.NewAdapterFactory(loader),
		probes.NewAdapterFactory(loader),
	}
}

// configLoader loads SM credentials from the grafanactl config + env vars.
type configLoader struct {
	configFile string
	ctxName    string
}

func (l *configLoader) bindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&l.configFile, "config", "", "Path to the configuration file to use")
	flags.StringVar(&l.ctxName, "context", "", "Name of the context to use")
}

// LoadSMConfig loads the SM base URL, token, and K8s namespace from config.
// Priority (highest first):
//  1. GRAFANA_SM_URL / GRAFANA_SM_TOKEN env vars (explicit)
//  2. GRAFANA_PROVIDER_SYNTH_SM_URL / _TOKEN env vars (generic provider prefix)
//  3. Config file: providers.synth.sm-url / sm-token
func (l *configLoader) LoadSMConfig(ctx context.Context) (string, string, string, error) {
	// Validate that the context exists (but don't require Grafana server config
	// since SM uses its own URL/token — only validate if grafana is configured).
	validator := func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		return nil
	}

	loaded, err := l.loadConfig(ctx, validator)
	if err != nil {
		return "", "", "", err
	}

	if !loaded.HasContext(loaded.CurrentContext) {
		return "", "", "", fmt.Errorf("context %q not found", loaded.CurrentContext)
	}

	curCtx := loaded.GetCurrentContext()

	// Extract SM credentials from providers config.
	var smURL, smToken string
	if prov := curCtx.Providers["synth"]; prov != nil {
		smURL = prov["sm-url"]
		smToken = prov["sm-token"]
	}

	// Explicit GRAFANA_SM_URL / GRAFANA_SM_TOKEN env vars override everything.
	if v := os.Getenv("GRAFANA_SM_URL"); v != "" {
		smURL = v
	}
	if v := os.Getenv("GRAFANA_SM_TOKEN"); v != "" {
		smToken = v
	}

	if smURL == "" {
		return "", "", "", errors.New(
			"SM URL not configured: set providers.synth.sm-url in config or GRAFANA_SM_URL env var")
	}
	if smToken == "" {
		return "", "", "", errors.New(
			"SM token not configured: set providers.synth.sm-token in config or GRAFANA_SM_TOKEN env var")
	}

	// Derive namespace from the Grafana config for K8s envelope metadata.
	// Falls back to "default" if no Grafana config is available.
	namespace := "default"
	if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
		restCfg := curCtx.ToRESTConfig(ctx)
		namespace = restCfg.Namespace
	}

	return smURL, smToken, namespace, nil
}

// LoadGrafanaConfig loads the REST config from the config file, applying
// env var overrides and context flags. Mirrors the SLO provider's implementation.
func (l *configLoader) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
	validator := func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		return cfg.GetCurrentContext().Validate()
	}

	loaded, err := l.loadConfig(ctx, validator)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}

	if !loaded.HasContext(loaded.CurrentContext) {
		return config.NamespacedRESTConfig{}, fmt.Errorf("context %q not found", loaded.CurrentContext)
	}

	return loaded.GetCurrentContext().ToRESTConfig(ctx), nil
}

// LoadConfig loads the full config from the config file, applying env var overrides
// and context flags. Used for datasource UID lookup from context settings.
func (l *configLoader) LoadConfig(ctx context.Context) (*config.Config, error) {
	validator := func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		return nil
	}

	loaded, err := l.loadConfig(ctx, validator)
	if err != nil {
		return nil, err
	}

	return &loaded, nil
}

// SaveMetricsDatasourceUID persists an auto-discovered Prometheus datasource UID to
// providers.synth.sm-metrics-datasource-uid in the config file.
func (l *configLoader) SaveMetricsDatasourceUID(ctx context.Context, uid string) error {
	loaded, err := l.loadConfig(ctx, func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		return nil
	})
	if err != nil {
		return err
	}

	curCtx := loaded.GetCurrentContext()
	if curCtx == nil {
		return fmt.Errorf("context %q not found", loaded.CurrentContext)
	}

	if curCtx.Providers == nil {
		curCtx.Providers = make(map[string]map[string]string)
	}
	if curCtx.Providers["synth"] == nil {
		curCtx.Providers["synth"] = make(map[string]string)
	}
	curCtx.Providers["synth"]["sm-metrics-datasource-uid"] = uid
	loaded.SetContext(loaded.CurrentContext, false, *curCtx)

	return config.Write(ctx, l.configSource(), loaded)
}

// loadConfig is the shared config-loading implementation.
// It applies env var overrides, the --context flag override, and the provided
// validator override, then calls config.Load.
func (l *configLoader) loadConfig(ctx context.Context, validator config.Override) (config.Config, error) {
	source := l.configSource()

	overrides := []config.Override{
		envOverride,
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

	overrides = append(overrides, validator)

	return config.Load(ctx, source, overrides...)
}

func (l *configLoader) configSource() config.Source {
	if l.configFile != "" {
		return config.ExplicitConfigFile(l.configFile)
	}
	return config.StandardLocation()
}

// envOverride applies environment variable overrides to the config.
// It ensures a current context exists, parses env vars into the context,
// and resolves GRAFANA_PROVIDER_{NAME}_{KEY} env vars into provider config.
func envOverride(cfg *config.Config) error {
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
}
