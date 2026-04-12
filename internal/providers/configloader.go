package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
)

// errNoConfigSource indicates no config file exists to write to.
var errNoConfigSource = errors.New("no config source available")

// CloudRESTConfig holds the resolved Grafana Cloud configuration needed to
// authenticate against cloud platform APIs.
type CloudRESTConfig struct {
	Token           string
	Stack           cloud.StackInfo
	Namespace       string
	ProviderConfigs map[string]map[string]string

	// RESTConfig is the underlying REST config from the named context, if available.
	// Providers should use rest.HTTPClientFor(RESTConfig) to create TLS-aware HTTP clients
	// when this is non-nil.
	RESTConfig *rest.Config
}

// HTTPClient returns a TLS-aware HTTP client derived from the REST config.
// Returns a new default HTTP client when no REST config is present.
func (c CloudRESTConfig) HTTPClient(ctx context.Context) (*http.Client, error) {
	if c.RESTConfig == nil {
		return httputils.NewDefaultClient(ctx), nil
	}
	return rest.HTTPClientFor(c.RESTConfig)
}

// ProviderConfig returns the configuration map for a specific provider, or nil if not set.
func (c CloudRESTConfig) ProviderConfig(name string) map[string]string {
	if c.ProviderConfigs == nil {
		return nil
	}
	return c.ProviderConfigs[name]
}

// ConfigLoader is a minimal config loading helper shared across providers.
// It avoids importing cmd/gcx/config (which would create an import cycle
// via internal/providers).
type ConfigLoader struct {
	configFile string
	ctxName    string
}

func (l *ConfigLoader) resolvedContextName(ctx context.Context) string {
	if l.ctxName != "" {
		return l.ctxName
	}
	return config.ContextNameFromCtx(ctx)
}

func contextSelectionOverride(ctxName string) config.Override {
	return func(cfg *config.Config) error {
		if ctxName == "" {
			return nil
		}
		if !cfg.HasContext(ctxName) {
			return config.ContextNotFound(ctxName)
		}
		cfg.CurrentContext = ctxName
		return nil
	}
}

func cloudEnvOverride(cfg *config.Config) error {
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

	return env.Parse(curCtx)
}

// contextMustExist is a config.Override that validates the current context exists.
func contextMustExist(cfg *config.Config) error {
	if !cfg.HasContext(cfg.CurrentContext) {
		return config.ContextNotFound(cfg.CurrentContext)
	}
	return nil
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

// LoadGrafanaConfig loads the REST config from the config file, applying
// env var overrides and context flags. It mirrors the logic in
// cmd/gcx/config.Options.LoadGrafanaConfig.
func (l *ConfigLoader) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{
		contextSelectionOverride(ctxName),
		envOverride,
		contextMustExist,
		func(cfg *config.Config) error {
			return cfg.GetCurrentContext().Validate()
		},
	}

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}

	restCfg := loaded.GetCurrentContext().ToRESTConfig(ctx)
	restCfg.WireTokenPersistence(ctx, l.configSource(), loaded.CurrentContext, loaded.Sources)

	return restCfg, nil
}

// LoadCloudConfig loads Grafana Cloud configuration, applying env var overrides.
// Unlike LoadGrafanaConfig it does not require grafana.server to be set.
// It validates that cloud.token is present, resolves the stack slug and GCOM URL,
// calls the GCOM API to discover stack info, and returns a CloudRESTConfig.
func (l *ConfigLoader) LoadCloudConfig(ctx context.Context) (CloudRESTConfig, error) {
	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{contextSelectionOverride(ctxName), cloudEnvOverride}

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
	if err != nil {
		return CloudRESTConfig{}, err
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
	client, err := cloud.NewGCOMClient(gcomURL, token)
	if err != nil {
		return CloudRESTConfig{}, fmt.Errorf("failed to create GCOM client: %w", err)
	}

	stack, err := client.GetStack(ctx, slug)
	if err != nil {
		return CloudRESTConfig{}, fmt.Errorf("failed to get stack info for %q: %w", slug, err)
	}

	// Derive namespace and REST config from grafana config if available.
	namespace := "default"
	var restCfg *rest.Config
	if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
		nrc := curCtx.ToRESTConfig(ctx)
		nrc.WireTokenPersistence(ctx, l.configSource(), loaded.CurrentContext, loaded.Sources)

		namespace = nrc.Namespace
		restCfg = &nrc.Config
	}

	return CloudRESTConfig{
		Token:           token,
		Stack:           stack,
		Namespace:       namespace,
		ProviderConfigs: curCtx.Providers,
		RESTConfig:      restCfg,
	}, nil
}

// configSource returns the config.Source to use for write-back operations.
// Mirrors the resolution logic in config.LoadLayered.
func (l *ConfigLoader) configSource() config.Source {
	if l.configFile != "" {
		return config.ExplicitConfigFile(l.configFile)
	}
	return config.StandardLocation()
}

// LoadProviderConfig loads the provider-specific config map and namespace for
// the named provider from the config file, applying GRAFANA_PROVIDER_<NAME>_<KEY>
// env var overrides. Returns (providerConfig, namespace, error).
func (l *ConfigLoader) LoadProviderConfig(ctx context.Context, providerName string) (map[string]string, string, error) {
	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{
		contextSelectionOverride(ctxName),
		envOverride,
		contextMustExist,
	}

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
	if err != nil {
		return nil, "", err
	}

	curCtx := loaded.GetCurrentContext()

	// Derive namespace from grafana config if available.
	namespace := "default"
	if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
		restCfg := curCtx.ToRESTConfig(ctx)
		namespace = restCfg.Namespace
	}

	providerCfg := curCtx.Providers[providerName]
	return providerCfg, namespace, nil
}

// SaveDatasourceUID persists a single datasource UID to
// contexts.[context].datasources.[kind] in the underlying config file.
//
// Unlike the read path, this writes the raw target file without applying env
// overrides or changing current-context, so auto-discovery does not flatten a
// layered config or persist env-derived values.
func (l *ConfigLoader) SaveDatasourceUID(ctx context.Context, kind, uid string) error {
	source, err := l.datasourceWriteSource()
	if errors.Is(err, errNoConfigSource) {
		logging.FromContext(ctx).Debug("no config file found; skipping datasource UID save",
			slog.String("datasource_kind", kind),
			slog.String("uid", uid),
		)
		return nil
	}
	if err != nil {
		return err
	}

	loaded, err := config.Load(ctx, source)
	if err != nil {
		return err
	}

	ctxName := l.ctxName
	if ctxName == "" {
		ctxName = config.ContextNameFromCtx(ctx)
	}
	if ctxName == "" {
		ctxName = loaded.CurrentContext
	}
	if ctxName == "" && loaded.HasContext(config.DefaultContextName) {
		ctxName = config.DefaultContextName
	}
	if !loaded.HasContext(ctxName) {
		return config.ContextNotFound(ctxName)
	}

	curCtx := loaded.Contexts[ctxName]
	if curCtx == nil {
		return fmt.Errorf("context %q not found", ctxName)
	}
	if curCtx.Datasources == nil {
		curCtx.Datasources = make(map[string]string)
	}
	curCtx.Datasources[kind] = uid
	loaded.SetContext(ctxName, false, *curCtx)

	return config.Write(ctx, source, loaded)
}

func (l *ConfigLoader) datasourceWriteSource() (config.Source, error) {
	if l.configFile != "" {
		return config.ExplicitConfigFile(l.configFile), nil
	}
	if envPath := os.Getenv(config.ConfigFileEnvVar); envPath != "" {
		return config.ExplicitConfigFile(envPath), nil
	}

	sources, err := config.DiscoverSources()
	if err != nil {
		return nil, err
	}
	if len(sources) > 1 {
		return nil, errors.New("multiple config files loaded; refusing to auto-save discovered datasource UID without --config")
	}
	if len(sources) == 1 {
		return config.ExplicitConfigFile(sources[0].Path), nil
	}

	return nil, errNoConfigSource
}

// SaveProviderConfig persists a single key-value pair to
// contexts.[current].providers.[providerName].[key] in the config file.
func (l *ConfigLoader) SaveProviderConfig(ctx context.Context, providerName, key, value string) error {
	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{
		contextSelectionOverride(ctxName),
		envOverride,
		contextMustExist,
	}

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
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
	if curCtx.Providers[providerName] == nil {
		curCtx.Providers[providerName] = make(map[string]string)
	}
	curCtx.Providers[providerName][key] = value
	loaded.SetContext(loaded.CurrentContext, false, *curCtx)

	return config.Write(ctx, l.configSource(), loaded)
}

// LoadFullConfig loads the full config from the config file, applying env var
// overrides and context flags. Returns a pointer to the resolved Config.
func (l *ConfigLoader) LoadFullConfig(ctx context.Context) (*config.Config, error) {
	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{
		contextSelectionOverride(ctxName),
		envOverride,
		contextMustExist,
	}

	loaded, err := config.LoadLayered(ctx, l.configFile, overrides...)
	if err != nil {
		return nil, err
	}

	return &loaded, nil
}
