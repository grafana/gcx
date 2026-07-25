package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
)

// errNoConfigSource indicates no config file exists to write to.
var errNoConfigSource = errors.New("no config source available")

// ErrAutoLocalProviderWriteback identifies an attempted implicit provider
// cache write to an auto-discovered repository config. Callers that treat
// cache persistence as best-effort may match this error and continue; explicit
// --config/GCX_CONFIG selection of the same path remains writable.
var ErrAutoLocalProviderWriteback = errors.New("provider cache write to auto-discovered local config is not allowed")

// AutoLocalProviderWritebackError carries the exact file that was protected
// from an implicit cache or credential write.
type AutoLocalProviderWritebackError struct {
	Path string
}

func (e AutoLocalProviderWritebackError) Error() string {
	return fmt.Sprintf("%s: %s; re-run with --config %q to authorize this file explicitly", ErrAutoLocalProviderWriteback, e.Path, e.Path)
}

func (e AutoLocalProviderWritebackError) Unwrap() error {
	return ErrAutoLocalProviderWriteback
}

// DirectProviderPolicy declares which provider values become outbound
// credential destinations. Loading through this policy keeps the destination,
// Grafana credential, and Cloud credential in one resolved config snapshot.
type DirectProviderPolicy struct {
	ProviderName string
	EndpointKeys []string

	// CredentialEnv is the environment variable that must accompany a
	// GRAFANA_PROVIDER_* endpoint override. Supplying a destination alone never
	// authorizes reusing a credential loaded from disk or the keychain.
	CredentialEnv string

	// RejectAutoLocal rejects the direct-auth path whenever its stack came from
	// an auto-discovered repository config, including endpoints discovered via
	// that stack's Grafana server. This is used by providers whose fallback
	// discovery result will receive a separate Cloud credential.
	RejectAutoLocal bool

	// RequireGrafana validates and materializes Grafana auth before returning.
	RequireGrafana bool
}

// DirectProviderSnapshot is a coherent, trust-checked view used by providers
// that send bearer credentials to non-Grafana endpoints.
type DirectProviderSnapshot struct {
	ProviderConfig map[string]string
	Namespace      string
	GrafanaConfig  *config.NamespacedRESTConfig

	// RuntimeEndpointOverrides records endpoint keys supplied through
	// GRAFANA_PROVIDER_* for provider-specific cache invalidation (notably k6).
	RuntimeEndpointOverrides map[string]bool

	// ResolveCloudConfig derives GCOM stack metadata and Cloud auth from the
	// exact same config snapshot. It performs no second config load.
	ResolveCloudConfig func(context.Context) (CloudRESTConfig, error)
}

// EndpointOverriddenByEnvironment reports whether the endpoint key was
// present in the process environment for this snapshot.
func (s DirectProviderSnapshot) EndpointOverriddenByEnvironment(key string) bool {
	return s.RuntimeEndpointOverrides[key]
}

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
//
// A nil *ConfigLoader is valid for every load path and behaves exactly like
// a zero-value loader (GCX_CONFIG, layered discovery, and ctx threading):
// provider Commands constructors take a flag-bound loader, and external
// callers — package tests in particular — pass nil. Only BindFlags and the
// setters require a non-nil receiver.
type ConfigLoader struct {
	configFile string
	ctxName    string
}

func (l *ConfigLoader) resolvedContextName(ctx context.Context) string {
	if l != nil && l.ctxName != "" {
		return l.ctxName
	}
	return config.ContextNameFromCtx(ctx)
}

// resolvedConfigFile returns the single explicit config selection for this
// operation. A loader-bound flag wins when a provider command owns --config;
// otherwise resource adapter loaders inherit the parent resources command's
// immutable context selection. An empty result retains GCX_CONFIG and layered
// discovery in config.LoadLayered.
func (l *ConfigLoader) resolvedConfigFile(ctx context.Context) string {
	if l != nil && l.configFile != "" {
		return l.configFile
	}
	return config.ConfigFileFromCtx(ctx)
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

	// ParseEnvIntoContext synthesizes an ephemeral cloud entry from the
	// GRAFANA_CLOUD_* env vars, winning over whatever the context references.
	return config.ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
}

// contextMustExist is a config.Override that validates the current context exists.
func contextMustExist(cfg *config.Config) error {
	if !cfg.HasContext(cfg.CurrentContext) {
		return config.ContextNotFound(cfg.CurrentContext)
	}
	return nil
}

// BindFlags registers the --config flag on the given flag set.
//
// --context is intentionally NOT bound here: it is owned by the root command
// as a persistent global flag and threaded into context.Context via
// PersistentPreRun. Re-binding it here would silently shadow the root binding,
// causing --context to be ignored on subcommands depending on its position.
func (l *ConfigLoader) BindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&l.configFile, "config", "", "Path to the configuration file to use")
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
	if err := config.ParseEnvIntoContext(curCtx); err != nil {
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
		if IsBlankProviderCredentialEnvironmentOverride(key, val) {
			continue
		}

		suffix := key[len(providerEnvPrefix):]
		nameParts := strings.SplitN(suffix, "_", 2)
		if len(nameParts) != 2 || nameParts[0] == "" || nameParts[1] == "" {
			continue
		}

		providerName := strings.ToLower(nameParts[0])
		configKey := strings.ReplaceAll(strings.ToLower(nameParts[1]), "_", "-")

		// The resolved Providers map is shared with the stack entry when the
		// context references one; env-derived values overlay the in-memory
		// view exactly as they overlaid the per-context map before.
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
	configFile := l.resolvedConfigFile(ctx)
	overrides := []config.Override{
		contextSelectionOverride(ctxName),
		envOverride,
		contextMustExist,
		func(cfg *config.Config) error {
			return cfg.GetCurrentContext().Validate(ctx)
		},
	}

	loaded, err := config.LoadLayered(ctx, configFile, overrides...)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}

	restCfg, err := loaded.GetCurrentContext().ToRESTConfig(ctx)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}
	restCfg.WireTokenPersistence(ctx, l.configSource(ctx), loaded.CurrentContext, loaded.GetCurrentContext().Stack, loaded.Sources)

	return restCfg, nil
}

// cloudBase holds the common pieces resolved by loadCloudBase.
type cloudBase struct {
	token  string
	client *cloud.GCOMClient
	loaded config.Config
	curCtx *config.Context
}

// loadCloudBase loads config, validates cloud auth, resolves the GCOM URL,
// and creates a GCOMClient. It is the shared preamble for both
// LoadCloudConfig (which additionally needs a stack slug) and
// LoadCloudTokenConfig (which does not).
func (l *ConfigLoader) loadCloudBase(ctx context.Context) (cloudBase, error) {
	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{contextSelectionOverride(ctxName), cloudEnvOverride}

	loaded, err := config.LoadLayered(ctx, l.resolvedConfigFile(ctx), overrides...)
	if err != nil {
		return cloudBase{}, err
	}

	curCtx := loaded.GetCurrentContext()

	if curCtx.CloudEntry == nil {
		return cloudBase{}, missingCloudAuthError(&loaded, curCtx)
	}
	token, err := curCtx.CloudEntry.ResolveToken()
	if err != nil {
		return cloudBase{}, err
	}
	if token == "" {
		return cloudBase{}, missingCloudAuthError(&loaded, curCtx)
	}
	apiURL := curCtx.ResolveCloudAPIURL()

	client, err := cloud.NewGCOMClient(apiURL, token)
	if err != nil {
		return cloudBase{}, fmt.Errorf("failed to create GCOM client: %w", err)
	}

	return cloudBase{
		token:  token,
		client: client,
		loaded: loaded,
		curCtx: curCtx,
	}, nil
}

// missingCloudAuthError explains how to get cloud auth onto the current
// context: bind an existing entry (named when exactly one exists) or run
// `gcx cloud login`. Cloud binding is optional at validation time, so this is
// the runtime error for cloud-dependent operations.
func missingCloudAuthError(cfg *config.Config, curCtx *config.Context) error {
	if curCtx.Cloud != "" {
		return fmt.Errorf("cloud entry %q has no token: run `gcx cloud login`, or set cloud.%s.token or GRAFANA_CLOUD_TOKEN",
			curCtx.Cloud, curCtx.Cloud)
	}
	if len(cfg.Cloud) == 1 {
		for name := range cfg.Cloud {
			return fmt.Errorf("context has no cloud auth: bind the existing entry with `gcx config set contexts.%s.cloud %s`, or run `gcx cloud login`", curCtx.Name, name)
		}
	}
	return errors.New("context has no cloud auth: run `gcx cloud login`, or set GRAFANA_CLOUD_TOKEN")
}

// CloudTokenConfig holds the minimal cloud credentials needed for
// org-level GCOM operations that do not target a specific stack.
type CloudTokenConfig struct {
	Token  string
	Client *cloud.GCOMClient
}

// LoadCloudTokenConfig loads Grafana Cloud configuration requiring only
// a cloud token (or GRAFANA_CLOUD_TOKEN). Unlike LoadCloudConfig it does not
// require a stack slug and does not call GetStack.
func (l *ConfigLoader) LoadCloudTokenConfig(ctx context.Context) (CloudTokenConfig, error) {
	base, err := l.loadCloudBase(ctx)
	if err != nil {
		return CloudTokenConfig{}, err
	}
	return CloudTokenConfig{
		Token:  base.token,
		Client: base.client,
	}, nil
}

// LoadCloudConfig loads Grafana Cloud configuration, applying env var overrides.
// Unlike LoadGrafanaConfig it does not require grafana.server to be set.
// It validates that a cloud token is present, resolves the stack slug and GCOM URL,
// calls the GCOM API to discover stack info, and returns a CloudRESTConfig.
func (l *ConfigLoader) LoadCloudConfig(ctx context.Context) (CloudRESTConfig, error) {
	base, err := l.loadCloudBase(ctx)
	if err != nil {
		return CloudRESTConfig{}, err
	}

	slug := base.curCtx.ResolveStackSlug()
	if slug == "" {
		return CloudRESTConfig{}, errors.New("cloud stack is not configured: set the stack's slug (gcx config set stacks.<name>.slug <slug>) or GRAFANA_CLOUD_STACK env var")
	}

	stack, err := base.client.GetStack(ctx, slug)
	if err != nil {
		return CloudRESTConfig{}, fmt.Errorf("failed to get stack info for %q: %w", slug, err)
	}

	namespace := "default"
	var restCfg *rest.Config
	if base.curCtx.Grafana != nil && !base.curCtx.Grafana.IsEmpty() {
		nrc, err := base.curCtx.ToRESTConfig(ctx)
		if err != nil {
			return CloudRESTConfig{}, err
		}
		nrc.WireTokenPersistence(ctx, l.configSource(ctx), base.loaded.CurrentContext, base.curCtx.Stack, base.loaded.Sources)

		namespace = nrc.Namespace
		restCfg = &nrc.Config
	}

	return CloudRESTConfig{
		Token:           base.token,
		Stack:           stack,
		Namespace:       namespace,
		ProviderConfigs: base.curCtx.Providers,
		RESTConfig:      restCfg,
	}, nil
}

// configSource returns the config.Source to use for write-back operations.
// Mirrors the resolution logic in config.LoadLayered.
func (l *ConfigLoader) configSource(ctx context.Context) config.Source {
	if configFile := l.resolvedConfigFile(ctx); configFile != "" {
		return config.ExplicitConfigFile(configFile)
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

	loaded, err := config.LoadLayered(ctx, l.resolvedConfigFile(ctx), overrides...)
	if err != nil {
		return nil, "", err
	}

	curCtx := loaded.GetCurrentContext()

	// Derive namespace from grafana config if available.
	namespace := "default"
	if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
		restCfg, err := curCtx.ToRESTConfig(ctx)
		if err != nil {
			return nil, "", err
		}
		namespace = restCfg.Namespace
	}

	providerCfg := curCtx.Providers[providerName]
	return providerCfg, namespace, nil
}

// LoadDirectProviderSnapshot loads one provider's direct-API destination and
// every credential it may use from one immutable layered-config snapshot. The
// trust checks run before the snapshot (and therefore any credential) is
// returned to provider code.
func (l *ConfigLoader) LoadDirectProviderSnapshot(ctx context.Context, policy DirectProviderPolicy) (DirectProviderSnapshot, error) {
	if policy.ProviderName == "" {
		return DirectProviderSnapshot{}, errors.New("direct provider policy requires a provider name")
	}
	if len(policy.EndpointKeys) > 0 && strings.TrimSpace(policy.CredentialEnv) == "" {
		return DirectProviderSnapshot{}, fmt.Errorf("direct provider policy for %s requires a runtime credential environment variable", policy.ProviderName)
	}

	ctxName := l.resolvedContextName(ctx)
	overrides := []config.Override{
		contextSelectionOverride(ctxName),
		envOverride,
		contextMustExist,
	}
	loaded, err := config.LoadLayered(ctx, l.resolvedConfigFile(ctx), overrides...)
	if err != nil {
		return DirectProviderSnapshot{}, err
	}
	curCtx := loaded.GetCurrentContext()
	if curCtx == nil {
		return DirectProviderSnapshot{}, config.ContextNotFound(loaded.CurrentContext)
	}

	providerCfg := cloneProviderValues(curCtx.Providers[policy.ProviderName])
	envEndpoints := make(map[string]bool, len(policy.EndpointKeys))
	for _, key := range policy.EndpointKeys {
		envKey := providerEnvironmentKey(policy.ProviderName, key)
		_, fromEnv := os.LookupEnv(envKey)
		envEndpoints[key] = fromEnv
		if providerCfg[key] != "" && fromEnv && strings.TrimSpace(os.Getenv(policy.CredentialEnv)) == "" {
			return DirectProviderSnapshot{}, fmt.Errorf(
				"refusing provider endpoint %s from %s without a matching runtime credential: set %s too, or put the endpoint in an explicitly selected --config file",
				key, envKey, policy.CredentialEnv,
			)
		}
	}
	if credentialKey := providerCredentialConfigKey(policy.ProviderName, policy.CredentialEnv); credentialKey != "" {
		if err := curCtx.DirectProviderCredentialRejection(policy.ProviderName, credentialKey); err != nil {
			return DirectProviderSnapshot{}, err
		}
	}

	localPath := autoLocalSourcePath(loaded.Sources)
	if policy.RejectAutoLocal && curCtx.StackFromAutoLocal() {
		return DirectProviderSnapshot{}, untrustedAutoLocalProviderError(policy.ProviderName, localPath)
	}
	for _, key := range policy.EndpointKeys {
		if providerCfg[key] != "" && curCtx.StackFromAutoLocal() {
			return DirectProviderSnapshot{}, untrustedAutoLocalProviderError(policy.ProviderName, localPath)
		}
	}

	if policy.RequireGrafana {
		if err := curCtx.Validate(ctx); err != nil {
			return DirectProviderSnapshot{}, err
		}
	}

	namespace := "default"
	var grafanaCfg *config.NamespacedRESTConfig
	if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
		nrc, err := curCtx.ToRESTConfig(ctx)
		if err != nil {
			return DirectProviderSnapshot{}, err
		}
		nrc.WireTokenPersistence(ctx, l.configSource(ctx), loaded.CurrentContext, curCtx.Stack, loaded.Sources)
		namespace = nrc.Namespace
		grafanaCfg = &nrc
	}

	return DirectProviderSnapshot{
		ProviderConfig:           providerCfg,
		Namespace:                namespace,
		GrafanaConfig:            grafanaCfg,
		RuntimeEndpointOverrides: envEndpoints,
		ResolveCloudConfig: func(resolveCtx context.Context) (CloudRESTConfig, error) {
			return l.resolveSnapshotCloudConfig(resolveCtx, loaded, curCtx, grafanaCfg)
		},
	}, nil
}

func cloneProviderValues(values map[string]string) map[string]string {
	return maps.Clone(values)
}

func providerEnvironmentKey(providerName, key string) string {
	provider := strings.ToUpper(strings.ReplaceAll(providerName, "-", "_"))
	field := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	return "GRAFANA_PROVIDER_" + provider + "_" + field
}

func providerCredentialConfigKey(providerName, credentialEnv string) string {
	prefix := "GRAFANA_PROVIDER_" + strings.ToUpper(strings.ReplaceAll(providerName, "-", "_")) + "_"
	if !strings.HasPrefix(credentialEnv, prefix) {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(credentialEnv, prefix), "_", "-"))
}

func autoLocalSourcePath(sources []config.ConfigSource) string {
	for _, source := range sources {
		if source.Type == "local" {
			return source.Path
		}
	}
	return config.LocalConfigFileName
}

func untrustedAutoLocalProviderError(providerName, path string) error {
	return fmt.Errorf(
		"refusing direct %s authentication from auto-discovered repository config %s; re-run with --config %q to authorize that file explicitly",
		providerName, path, path,
	)
}

func (l *ConfigLoader) resolveSnapshotCloudConfig(
	ctx context.Context,
	loaded config.Config,
	curCtx *config.Context,
	grafanaCfg *config.NamespacedRESTConfig,
) (CloudRESTConfig, error) {
	if curCtx.CloudEntry == nil {
		return CloudRESTConfig{}, missingCloudAuthError(&loaded, curCtx)
	}
	token, err := curCtx.CloudEntry.ResolveToken()
	if err != nil {
		return CloudRESTConfig{}, err
	}
	if token == "" {
		return CloudRESTConfig{}, missingCloudAuthError(&loaded, curCtx)
	}
	client, err := cloud.NewGCOMClient(curCtx.ResolveCloudAPIURL(), token)
	if err != nil {
		return CloudRESTConfig{}, fmt.Errorf("failed to create GCOM client: %w", err)
	}

	slug := curCtx.ResolveStackSlug()
	if slug == "" {
		return CloudRESTConfig{}, errors.New("cloud stack is not configured: set the stack's slug (gcx config set stacks.<name>.slug <slug>) or GRAFANA_CLOUD_STACK env var")
	}
	stack, err := client.GetStack(ctx, slug)
	if err != nil {
		return CloudRESTConfig{}, fmt.Errorf("failed to get stack info for %q: %w", slug, err)
	}

	result := CloudRESTConfig{
		Token:           token,
		Stack:           stack,
		Namespace:       "default",
		ProviderConfigs: curCtx.Providers,
	}
	if grafanaCfg != nil {
		result.Namespace = grafanaCfg.Namespace
		result.RESTConfig = &grafanaCfg.Config
	}
	return result, nil
}

// SaveDatasourceUID persists a single datasource UID to
// contexts.[context].datasources.[kind] in the underlying config file.
//
// Unlike the read path, this writes the raw target file without applying env
// overrides or changing current-context, so auto-discovery does not flatten a
// layered config or persist env-derived values.
func (l *ConfigLoader) SaveDatasourceUID(ctx context.Context, kind, uid string) error {
	source, sourceInfo, err := l.writeBackSource(ctx, "discovered datasource UID")
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

	writeCtx := config.ContextWithConfigSource(ctx, sourceInfo)
	loaded, err := config.Load(writeCtx, source)
	if err != nil {
		return err
	}

	ctxName := l.resolvedContextName(ctx)
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

	return config.Write(writeCtx, source, loaded)
}

func (l *ConfigLoader) writeBackSource(ctx context.Context, description string) (config.Source, config.ConfigSource, error) {
	return l.writeBackSourceWithPolicy(ctx, description, false)
}

func (l *ConfigLoader) writeBackSourceWithPolicy(ctx context.Context, description string, rejectAutoLocal bool) (config.Source, config.ConfigSource, error) {
	if configFile := l.resolvedConfigFile(ctx); configFile != "" {
		return config.ExplicitConfigFile(configFile), config.ConfigSource{Path: configFile, Type: "explicit"}, nil
	}
	if envPath := os.Getenv(config.ConfigFileEnvVar); envPath != "" {
		return config.ExplicitConfigFile(envPath), config.ConfigSource{Path: envPath, Type: "explicit"}, nil
	}

	sources, err := config.DiscoverSources()
	if err != nil {
		return nil, config.ConfigSource{}, err
	}
	if rejectAutoLocal {
		for _, sourceInfo := range sources {
			if sourceInfo.Type == "local" {
				return nil, config.ConfigSource{}, AutoLocalProviderWritebackError{Path: sourceInfo.Path}
			}
		}
	}
	if len(sources) > 1 {
		return nil, config.ConfigSource{}, fmt.Errorf("multiple config files loaded; refusing to auto-save %s without --config", description)
	}
	if len(sources) == 1 {
		return config.ExplicitConfigFile(sources[0].Path), sources[0], nil
	}

	return nil, config.ConfigSource{}, errNoConfigSource
}

// SaveProviderConfig persists a single key-value pair to the selected
// context's stack entry (stacks.[name].providers.[providerName].[key]) in one
// raw config file, creating a stack named after the context when it has none.
// It deliberately does not load the layered/env-overridden read view: writing
// that view would flatten layers and persist process-local environment values.
func (l *ConfigLoader) SaveProviderConfig(ctx context.Context, providerName, key, value string) error {
	source, sourceInfo, err := l.writeBackSourceWithPolicy(ctx, "provider config", true)
	if errors.Is(err, errNoConfigSource) {
		logging.FromContext(ctx).Debug("no config file found; skipping provider config save",
			slog.String("provider", providerName),
			slog.String("key", key),
		)
		return nil
	}
	if err != nil {
		return err
	}
	writeCtx := config.ContextWithConfigSource(ctx, sourceInfo)
	loaded, err := config.Load(writeCtx, source)
	if err != nil {
		return err
	}

	ctxName := l.resolvedContextName(ctx)
	if ctxName == "" {
		ctxName = loaded.CurrentContext
	}
	if ctxName == "" && loaded.HasContext(config.DefaultContextName) {
		ctxName = config.DefaultContextName
	}
	if !loaded.HasContext(ctxName) {
		return config.ContextNotFound(ctxName)
	}

	// Load resolves keychain values eagerly only for current-context. Resolve an
	// explicitly selected non-current context before mutating its stack so Write
	// can round-trip any sentinels safely.
	loaded.ResolveContext(ctxName)
	curCtx := loaded.Contexts[ctxName]
	if curCtx == nil {
		return fmt.Errorf("context %q not found", ctxName)
	}

	stack := curCtx.StackEntry
	if stack == nil {
		loaded.SetStack(curCtx.Name, config.StackConfig{})
		curCtx.Stack = curCtx.Name
		loaded.Resolve()
		stack = curCtx.StackEntry
	}

	finishMutation := loaded.PrepareSecretPathMutation("stacks." + curCtx.Stack + ".providers." + providerName + "." + key)
	if stack.Providers == nil {
		stack.Providers = make(map[string]map[string]string)
	}
	if stack.Providers[providerName] == nil {
		stack.Providers[providerName] = make(map[string]string)
	}
	stack.Providers[providerName][key] = value
	if err := finishMutation(); err != nil {
		return err
	}

	return config.Write(writeCtx, source, loaded)
}

// LoadConfigTolerant loads the raw config, applying env var overrides and the
// resolved --context/--config flags, without validating the resulting context.
// It mirrors cmd/gcx/config.Options.LoadConfigTolerant exactly: context
// selection first, then context-scoped env overrides, then any caller-supplied
// overrides. Used by the assistant A2A command tree, which loads the raw
// context to read grafana.ProxyEndpoint/OAuthToken directly for its OAuth-PKCE
// streaming transport.
func (l *ConfigLoader) LoadConfigTolerant(ctx context.Context, extraOverrides ...config.Override) (config.Config, error) {
	overrides := make([]config.Override, 0, 2+len(extraOverrides))
	overrides = append(overrides,
		contextSelectionOverride(l.resolvedContextName(ctx)),
		envOverride,
	)
	overrides = append(overrides, extraOverrides...)
	return config.LoadLayered(ctx, l.resolvedConfigFile(ctx), overrides...)
}

// LoadConfig loads and validates the raw config. It mirrors
// cmd/gcx/config.Options.LoadConfig: LoadConfigTolerant plus a validator that
// requires the current context to exist and pass Validate().
func (l *ConfigLoader) LoadConfig(ctx context.Context) (config.Config, error) {
	validator := func(cfg *config.Config) error {
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}
		return cfg.GetCurrentContext().Validate(ctx)
	}
	return l.LoadConfigTolerant(ctx, validator)
}

// ConfigSource returns the config.Source to use for write-back operations,
// mirroring cmd/gcx/config.Options.ConfigSource. It honors an explicit config
// file carried by ctx, so the assistant A2A path re-reads and persists refreshed
// OAuth tokens to the same source it loaded.
func (l *ConfigLoader) ConfigSource(ctx context.Context) config.Source {
	return l.configSource(ctx)
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

	loaded, err := config.LoadLayered(ctx, l.resolvedConfigFile(ctx), overrides...)
	if err != nil {
		return nil, err
	}

	return &loaded, nil
}
