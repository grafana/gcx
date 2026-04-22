package config

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

const (
	// DefaultContextName is the name of the default context.
	DefaultContextName = "default"
)

// Config holds the information needed to connect to remote Grafana instances.
type Config struct {
	// Source contains the path to the config file parsed to populate this struct.
	Source string `json:"-" yaml:"-"`

	// Sources lists all config files that were discovered and merged to produce
	// this config. Populated by LoadLayered.
	Sources []ConfigSource `json:"-" yaml:"-"`

	// Contexts is a map of context configurations, indexed by name.
	Contexts map[string]*Context `json:"contexts" yaml:"contexts"`

	// CurrentContext is the name of the context currently in use.
	CurrentContext string `json:"current-context" yaml:"current-context"`
}

func (config *Config) HasContext(name string) bool {
	return config.Contexts[name] != nil
}

// GetCurrentContext returns the current context.
// If the current context is not set, it returns an error.
func (config *Config) GetCurrentContext() *Context {
	return config.Contexts[config.CurrentContext]
}

// SetContext adds a new context to the Grafana config.
// If a context with the same name already exists, it is overwritten.
func (config *Config) SetContext(name string, makeCurrent bool, context Context) {
	if config.Contexts == nil {
		config.Contexts = make(map[string]*Context)
	}

	config.Contexts[name] = &context

	if makeCurrent {
		config.CurrentContext = name
	}
}

// CloudConfig holds Grafana Cloud platform credentials and configuration.
type CloudConfig struct {
	// Token is a Grafana Cloud API token used to authenticate against GCOM.
	Token string `datapolicy:"secret" env:"GRAFANA_CLOUD_TOKEN" json:"token,omitempty" yaml:"token,omitempty"`

	// Stack is the Grafana Cloud stack slug (e.g. "mystack").
	// Optional: if not set, the slug may be derived from Grafana.Server.
	Stack string `env:"GRAFANA_CLOUD_STACK" json:"stack,omitempty" yaml:"stack,omitempty"`

	// APIUrl is the base URL of the Grafana Cloud API (GCOM).
	// Optional: defaults to "https://grafana.com".
	APIUrl string `env:"GRAFANA_CLOUD_API_URL" json:"api-url,omitempty" yaml:"api-url,omitempty"`
}

// Context holds the information required to connect to a remote Grafana instance.
type Context struct {
	Name string `json:"-" yaml:"-"`

	Grafana *GrafanaConfig `json:"grafana,omitempty" yaml:"grafana,omitempty"`

	Cloud *CloudConfig `json:"cloud,omitempty" yaml:"cloud,omitempty"`

	// DefaultPrometheusDatasource is the UID of the default Prometheus datasource to use for queries.
	DefaultPrometheusDatasource string `json:"default-prometheus-datasource,omitempty" yaml:"default-prometheus-datasource,omitempty"`

	// DefaultLokiDatasource is the UID of the default Loki datasource to use for queries.
	DefaultLokiDatasource string `json:"default-loki-datasource,omitempty" yaml:"default-loki-datasource,omitempty"`

	// DefaultPyroscopeDatasource is the UID of the default Pyroscope datasource to use for queries.
	DefaultPyroscopeDatasource string `json:"default-pyroscope-datasource,omitempty" yaml:"default-pyroscope-datasource,omitempty"`

	// DefaultTempoDatasource is the UID of the default Tempo datasource to use for queries.
	DefaultTempoDatasource string `json:"default-tempo-datasource,omitempty" yaml:"default-tempo-datasource,omitempty"`

	// Datasources holds per-kind default datasource UIDs, indexed by datasource kind (e.g. "prometheus", "loki").
	// Takes precedence over the legacy DefaultXxxDatasource fields when both are set.
	Datasources map[string]string `json:"datasources,omitempty" yaml:"datasources,omitempty"`

	// Providers holds per-provider configuration, indexed by provider name.
	// Each provider has a map of string key-value pairs.
	// Secret fields are selectively redacted by providers.RedactSecrets using
	// each provider's ConfigKey metadata.
	Providers map[string]map[string]string `json:"providers,omitempty" yaml:"providers,omitempty"`
}

func (context *Context) Validate() error {
	if context.Grafana == nil || context.Grafana.IsEmpty() {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'", context.Name),
			Message: "grafana config is required",
		}
	}

	return context.Grafana.Validate(context.Name)
}

// ToRESTConfig returns a REST config for the context.
func (context *Context) ToRESTConfig(ctx context.Context) NamespacedRESTConfig {
	return NewNamespacedRESTConfig(ctx, *context)
}

// ResolveStackSlug returns the Grafana Cloud stack slug for this context.
// It checks Cloud.Stack first; if not set, attempts to derive the slug from
// Grafana.Server by extracting the subdomain from *.grafana.net or *.grafana-dev.net URLs.
// Returns "" if neither source yields a slug.
func (context *Context) ResolveStackSlug() string {
	if context.Cloud != nil && context.Cloud.Stack != "" {
		return context.Cloud.Stack
	}

	if context.Grafana == nil || context.Grafana.Server == "" {
		return ""
	}

	slug, _ := StackSlugFromServerURL(context.Grafana.Server)
	return slug
}

// StackSlugFromServerURL attempts to extract a Grafana Cloud stack slug from
// a server URL. It returns the slug and true for *.grafana.net,
// *.grafana-dev.net, and *.grafana-ops.net URLs, or ("", false) for anything else.
// For non-prod Grafana-run environments, an env suffix is appended to the slug
// to prevent context-name collisions: "-dev" for *.grafana-dev.net, "-ops" for
// *.grafana-ops.net. *.grafana.net (prod) is returned unchanged.
func StackSlugFromServerURL(serverURL string) (string, bool) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return "", false
	}

	host := parsed.Hostname()
	for _, entry := range []struct {
		suffix string
		envTag string // appended to the slug for non-prod Grafana-run environments
	}{
		{".grafana.net", ""},
		{".grafana-dev.net", "-dev"},
		{".grafana-ops.net", "-ops"},
	} {
		slug, ok := strings.CutSuffix(host, entry.suffix)
		if !ok {
			continue
		}
		// For regional subdomains like "mystack.us.grafana.net",
		// CutSuffix returns "mystack.us". Take only the first component.
		if i := strings.Index(slug, "."); i >= 0 {
			slug = slug[:i]
		}
		if slug == "" {
			continue
		}
		return slug + entry.envTag, true
	}

	return "", false
}

// ContextNameFromServerURL derives a context name from a Grafana server URL.
// For Grafana Cloud URLs, it returns the stack slug (with env suffix for
// -dev/-ops). For other URLs, dots in the hostname are replaced with hyphens
// to keep the name shell-friendly. Returns DefaultContextName if the URL
// cannot be parsed.
func ContextNameFromServerURL(serverURL string) string {
	if slug, ok := StackSlugFromServerURL(serverURL); ok {
		return slug
	}

	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Hostname() == "" {
		return DefaultContextName
	}

	return strings.ReplaceAll(parsed.Hostname(), ".", "-")
}

// ResolveGCOMURL returns the Grafana Cloud API (GCOM) base URL for this context.
// If Cloud.APIUrl is set, it is returned prefixed with "https://".
// Otherwise, "https://grafana.com" is returned.
func (context *Context) ResolveGCOMURL() string {
	if context.Cloud != nil && context.Cloud.APIUrl != "" {
		apiURL := context.Cloud.APIUrl
		if !strings.HasPrefix(apiURL, "https://") && !strings.HasPrefix(apiURL, "http://") {
			apiURL = "https://" + apiURL
		}
		if strings.HasPrefix(apiURL, "http://") {
			slog.Warn("GCOM API URL uses http:// — cloud tokens may be sent unencrypted", "url", apiURL)
		}
		return apiURL
	}

	return "https://grafana.com"
}

type GrafanaConfig struct {
	// Server is the address of the Grafana server (https://hostname:port/path).
	// Required.
	Server string `env:"GRAFANA_SERVER" json:"server,omitempty" yaml:"server,omitempty"`

	// User to authenticate as with basic authentication.
	// Optional.
	User string `env:"GRAFANA_USER" json:"user,omitempty" yaml:"user,omitempty"`
	// Password to use when using with basic authentication.
	// Optional.
	Password string `datapolicy:"secret" env:"GRAFANA_PASSWORD" json:"password,omitempty" yaml:"password,omitempty"`

	// APIToken is a service account token.
	// See https://grafana.com/docs/grafana/latest/administration/service-accounts/#add-a-token-to-a-service-account-in-grafana
	// Note: if defined, the API Token takes precedence over basic auth credentials.
	// Optional.
	APIToken string `datapolicy:"secret" env:"GRAFANA_TOKEN" json:"token,omitempty" yaml:"token,omitempty"`

	// ProxyEndpoint is the assistant backend URL used as a reverse proxy for
	// OAuth-authenticated requests. Set automatically by `auth login`.
	// This may differ from Server when cloud routing directs CLI traffic through
	// a separate endpoint (e.g. the assistant app backend).
	ProxyEndpoint string `env:"GRAFANA_PROXY_ENDPOINT" json:"proxy-endpoint,omitempty" yaml:"proxy-endpoint,omitempty"`

	// OAuthToken is the OAuth access token (gat_) obtained via `auth login`.
	OAuthToken string `datapolicy:"secret" json:"oauth-token,omitempty" yaml:"oauth-token,omitempty"`

	// OAuthRefreshToken is the refresh token (gar_) for renewing OAuthToken.
	OAuthRefreshToken string `datapolicy:"secret" json:"oauth-refresh-token,omitempty" yaml:"oauth-refresh-token,omitempty"`

	// OAuthTokenExpiresAt is the OAuthToken expiration time in RFC3339 format.
	OAuthTokenExpiresAt string `json:"oauth-token-expires-at,omitempty" yaml:"oauth-token-expires-at,omitempty"`

	// OAuthRefreshExpiresAt is the OAuthRefreshToken expiration time in RFC3339 format.
	OAuthRefreshExpiresAt string `json:"oauth-refresh-expires-at,omitempty" yaml:"oauth-refresh-expires-at,omitempty"`

	// AuthMethod is the authentication method stored by gcx login: "oauth", "token", or "basic".
	// Empty string is valid for legacy configs; readers should call InferredAuthMethod() in that case.
	AuthMethod string `json:"auth-method,omitempty" yaml:"auth-method,omitempty"`

	// OrgID specifies the organization targeted by this config.
	// Note: required when targeting an on-prem Grafana instance.
	// See StackID for Grafana Cloud instances.
	OrgID int64 `env:"GRAFANA_ORG_ID" json:"org-id,omitempty" yaml:"org-id,omitempty"`

	// StackID specifies the Grafana Cloud stack targeted by this config.
	// Note: required when targeting a Grafana Cloud instance.
	// See OrgID for on-prem Grafana instances.
	StackID int64 `env:"GRAFANA_STACK_ID" json:"stack-id,omitempty" yaml:"stack-id,omitempty"`

	// TLS contains TLS-related configuration settings.
	TLS *TLS `json:"tls,omitempty" yaml:"tls,omitempty"`
}

func (grafana GrafanaConfig) validateNamespace(contextName string) error {
	if grafana.OrgID != 0 {
		return nil
	}

	discoveredStackID, discoveryErr := DiscoverStackID(context.Background(), grafana)

	if grafana.StackID == 0 {
		if discoveryErr != nil {
			return ValidationError{
				Path:    fmt.Sprintf("$.contexts.'%s'.grafana", contextName),
				Message: fmt.Sprintf("missing contexts.%[1]s.grafana.org-id or contexts.%[1]s.grafana.stack-id", contextName),
				Suggestions: []string{
					"Specify the Grafana Org ID for on-prem Grafana",
					"Specify the Grafana Cloud Stack ID for Grafana Cloud",
					"Find your Stack ID at grafana.com under your stack's details page",
				},
			}
		}

		return nil
	}

	// If discovery failed but grafana.StackID is set, we proceed with the configured StackID
	//nolint:nilerr // We intentionally ignore the error when StackID is configured
	if discoveryErr != nil {
		return nil
	}

	if discoveredStackID != grafana.StackID {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'.grafana", contextName),
			Message: fmt.Sprintf("mismatched contexts.%[1]s.grafana.stack-id, discovered %d - was %d in config", contextName, discoveredStackID, grafana.StackID),
			Suggestions: []string{
				"Specify the correct Grafana Cloud Stack ID for Grafana Cloud or omit the stack-id param",
			},
		}
	}

	return nil
}

func (grafana GrafanaConfig) Validate(contextName string) error {
	if grafana.Server == "" {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'.grafana", contextName),
			Message: "server is required",
			Suggestions: []string{
				"Set the address of the Grafana server to connect to",
			},
		}
	}

	hasProxy := grafana.ProxyEndpoint != ""
	hasOAuth := grafana.OAuthToken != ""
	if hasProxy != hasOAuth {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'.grafana", contextName),
			Message: "incomplete OAuth config: proxy-endpoint and oauth-token must both be set",
			Suggestions: []string{
				"Run `gcx login` to complete the OAuth flow",
				"Or remove partial OAuth fields from the config",
			},
		}
	}

	if err := grafana.validateNamespace(contextName); err != nil {
		return err
	}

	return nil
}

func (grafana GrafanaConfig) IsEmpty() bool {
	return grafana == GrafanaConfig{}
}

// InferredAuthMethod returns the effective authentication method for this config.
// When AuthMethod is set, it is returned verbatim. Otherwise, the method is inferred
// from populated credential fields: OAuthToken => "oauth"; APIToken => "token";
// User or Password => "basic"; no credentials => "unknown".
func (grafana GrafanaConfig) InferredAuthMethod() string {
	if grafana.AuthMethod != "" {
		return grafana.AuthMethod
	}
	if grafana.OAuthToken != "" {
		return "oauth"
	}
	if grafana.APIToken != "" {
		return "token"
	}
	if grafana.User != "" || grafana.Password != "" {
		return "basic"
	}
	return "unknown"
}

// TLS contains settings to enable transport layer security.
type TLS struct {
	// InsecureSkipTLSVerify disables the validation of the server's SSL certificate.
	// Enabling this will make your HTTPS connections insecure.
	Insecure bool `json:"insecure-skip-verify,omitempty" yaml:"insecure-skip-verify,omitempty"`

	// ServerName is passed to the server for SNI and is used in the client to check server
	// certificates against. If ServerName is empty, the hostname used to contact the
	// server is used.
	ServerName string `json:"server-name,omitempty" yaml:"server-name,omitempty"`

	// CertData holds PEM-encoded bytes (typically read from a client certificate file).
	// Note: this value is base64-encoded in the config file and will be
	// automatically decoded.
	CertData []byte `json:"cert-data,omitempty" yaml:"cert-data,omitempty"`
	// KeyData holds PEM-encoded bytes (typically read from a client certificate key file).
	// Note: this value is base64-encoded in the config file and will be
	// automatically decoded.
	KeyData []byte `datapolicy:"secret" json:"key-data,omitempty" yaml:"key-data,omitempty"`
	// CAData holds PEM-encoded bytes (typically read from a root certificates bundle).
	// Note: this value is base64-encoded in the config file and will be
	// automatically decoded.
	CAData []byte `json:"ca-data,omitempty" yaml:"ca-data,omitempty"`

	// NextProtos is a list of supported application level protocols, in order of preference.
	// Used to populate tls.Config.NextProtos.
	// To indicate to the server http/1.1 is preferred over http/2, set to ["http/1.1", "h2"] (though the server is free to ignore that preference).
	// To use only http/1.1, set to ["http/1.1"].
	NextProtos []string `json:"next-protos,omitempty" yaml:"next-protos,omitempty"`
}

func (cfg *TLS) ToStdTLSConfig() *tls.Config {
	// TODO: CertData, KeyData, CAData
	return &tls.Config{
		//nolint:gosec
		InsecureSkipVerify: cfg.Insecure,
		ServerName:         cfg.ServerName,
		NextProtos:         cfg.NextProtos,
	}
}

// Minify returns a trimmed down version of the given configuration containing
// only the current context and the relevant options it directly depends on.
func Minify(config Config) (Config, error) {
	minified := config

	if config.CurrentContext == "" {
		return Config{}, errors.New("current-context must be defined in order to minify")
	}

	minified.Contexts = make(map[string]*Context, 1)
	for name, ctx := range config.Contexts {
		if name == minified.CurrentContext {
			minified.Contexts[name] = ctx
		}
	}

	return minified, nil
}
