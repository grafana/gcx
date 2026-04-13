package config

import (
	"context"
	"net/http"
	"strings"
	"time"

	authlib "github.com/grafana/authlib/types"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
	"github.com/grafana/gcx/internal/version"
	"k8s.io/client-go/rest"
)

// NamespacedRESTConfig is a REST config with a namespace.
// TODO: move to app SDK?
type NamespacedRESTConfig struct {
	rest.Config

	Namespace string

	// GrafanaURL is the user-facing Grafana server URL (e.g., "https://mystack.grafana.net").
	// This is always the original grafana.server value, even when Host is rewritten
	// to a proxy endpoint for OAuth mode. Use this for deep link URLs, not Host.
	GrafanaURL string

	// oauthTransport holds a reference to the RefreshTransport when OAuth proxy
	// mode is active, allowing callers to wire the OnRefresh callback after
	// construction (Option C: call-site wiring).
	oauthTransport *auth.RefreshTransport
}

// IsOAuthProxy reports whether the config is using OAuth proxy mode.
func (n *NamespacedRESTConfig) IsOAuthProxy() bool {
	return n.oauthTransport != nil
}

// SetOnRefresh registers a callback that is invoked after a successful OAuth
// token refresh. This allows the call site (which has access to the config
// source) to persist refreshed tokens back to the config file.
// No-op if the config is not using OAuth proxy mode.
func (n *NamespacedRESTConfig) SetOnRefresh(fn auth.TokenRefresher) {
	if n.oauthTransport != nil {
		n.oauthTransport.OnRefresh = fn
	}
}

// WireTokenPersistence registers an OnRefresh callback that reloads the config
// from the most appropriate source, updates the OAuth token fields for
// contextName, and writes back.
// No-op if the config is not using OAuth proxy mode.
func (n *NamespacedRESTConfig) WireTokenPersistence(ctx context.Context, source Source, contextName string, sources []ConfigSource) {
	persistSource := ResolveTokenPersistenceSource(ctx, source, contextName, sources)

	n.SetOnRefresh(func(token, refreshToken, expiresAt, refreshExpiresAt string) error {
		fresh, err := Load(ctx, persistSource)
		if err != nil {
			return err
		}

		c := fresh.Contexts[contextName]
		if c == nil {
			c = &Context{}
			if fresh.Contexts == nil {
				fresh.Contexts = make(map[string]*Context)
			}
			fresh.Contexts[contextName] = c
		}
		if c.Grafana == nil {
			c.Grafana = &GrafanaConfig{}
		}

		c.Grafana.OAuthToken = token
		c.Grafana.OAuthRefreshToken = refreshToken
		c.Grafana.OAuthTokenExpiresAt = expiresAt
		c.Grafana.OAuthRefreshExpiresAt = refreshExpiresAt
		return Write(ctx, persistSource, fresh)
	})
}

// RESTConfigForContext constructs a namespaced REST config for the named
// context and wires OAuth token persistence when proxy auth is enabled.
func (cfg Config) RESTConfigForContext(ctx context.Context, contextName string, source Source) (NamespacedRESTConfig, error) {
	if !cfg.HasContext(contextName) {
		return NamespacedRESTConfig{}, ContextNotFound(contextName)
	}

	restCfg := cfg.Contexts[contextName].ToRESTConfig(ctx)
	restCfg.WireTokenPersistence(ctx, source, contextName, cfg.Sources)

	return restCfg, nil
}

// ResolveTokenPersistenceSource picks the best config file to persist rotated OAuth tokens.
// It returns a Source pointing to the highest-priority file that already contains OAuth fields
// for the given context, falling back to the user-level config or the provided fallback.
func ResolveTokenPersistenceSource(ctx context.Context, fallback Source, contextName string, sources []ConfigSource) Source {
	if len(sources) == 0 {
		return fallback
	}

	// Explicit mode bypasses layered config and should always persist to the explicit file.
	for _, src := range sources {
		if src.Type == "explicit" {
			return ExplicitConfigFile(src.Path)
		}
	}

	if src, ok := pickHighestSourceForContext(ctx, sources, contextName, contextHasOAuthFields); ok {
		return ExplicitConfigFile(src.Path)
	}
	if src, ok := pickHighestSourceForContext(ctx, sources, contextName, contextExists); ok {
		return ExplicitConfigFile(src.Path)
	}

	// No source has the context; default to user layer when available.
	for i := len(sources) - 1; i >= 0; i-- {
		if sources[i].Type == "user" {
			return ExplicitConfigFile(sources[i].Path)
		}
	}

	return fallback
}

func pickHighestSourceForContext(ctx context.Context, sources []ConfigSource, contextName string, match func(*Context) bool) (ConfigSource, bool) {
	// DiscoverSources returns low→high precedence, so scan in reverse.
	for i := len(sources) - 1; i >= 0; i-- {
		cfg, err := Load(ctx, ExplicitConfigFile(sources[i].Path))
		if err != nil {
			continue
		}
		if c := cfg.Contexts[contextName]; c != nil && match(c) {
			return sources[i], true
		}
	}
	return ConfigSource{}, false
}

func contextExists(c *Context) bool {
	return c != nil
}

func contextHasOAuthFields(c *Context) bool {
	if c == nil || c.Grafana == nil {
		return false
	}
	g := c.Grafana
	return g.OAuthToken != "" ||
		g.OAuthRefreshToken != "" ||
		g.OAuthTokenExpiresAt != "" ||
		g.OAuthRefreshExpiresAt != "" ||
		g.ProxyEndpoint != ""
}

// parseRFC3339OrZero parses an RFC3339 timestamp, returning the zero time on
// empty input or parse failure.
func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// NewNamespacedRESTConfig creates a new namespaced REST config.
func NewNamespacedRESTConfig(ctx context.Context, cfg Context) NamespacedRESTConfig {
	rcfg := rest.Config{
		UserAgent:       version.UserAgent(),
		Host:            strings.TrimSuffix(cfg.Grafana.Server, "/"),
		APIPath:         "/apis",
		TLSClientConfig: rest.TLSClientConfig{},
		// TODO: make configurable
		QPS:   50,
		Burst: 100,
	}

	if cfg.Grafana.TLS != nil {
		// Kubernetes really is wonderful, huh.
		// tl;dr it has its own TLSClientConfig,
		// and it's not compatible with the one from the "crypto/tls" package.
		rcfg.TLSClientConfig = rest.TLSClientConfig{
			Insecure:   cfg.Grafana.TLS.Insecure,
			ServerName: cfg.Grafana.TLS.ServerName,
			CertData:   cfg.Grafana.TLS.CertData,
			KeyData:    cfg.Grafana.TLS.KeyData,
			CAData:     cfg.Grafana.TLS.CAData,
			NextProtos: cfg.Grafana.TLS.NextProtos,
		}
	}

	// Authentication
	var oauthTransport *auth.RefreshTransport
	switch {
	case cfg.Grafana.ProxyEndpoint != "" && cfg.Grafana.OAuthToken != "":
		// OAuth proxy mode: route requests through the assistant backend proxy.
		// The ProxyEndpoint may differ from Server (e.g. cloud routing through
		// the assistant backend), so it is stored as a separate config field.
		// RefreshTransport handles bearer auth and token renewal; no BearerToken
		// on rcfg to avoid client-go adding a redundant auth layer.
		rcfg.Host = strings.TrimSuffix(cfg.Grafana.ProxyEndpoint, "/") + "/api/cli/v1/proxy"

		// Zero time for ExpiresAt triggers an immediate refresh on first request.
		expiresAt := parseRFC3339OrZero(cfg.Grafana.OAuthTokenExpiresAt)
		refreshExpiresAt := parseRFC3339OrZero(cfg.Grafana.OAuthRefreshExpiresAt)
		oauthTransport = &auth.RefreshTransport{
			ProxyEndpoint:    cfg.Grafana.ProxyEndpoint,
			Token:            cfg.Grafana.OAuthToken,
			RefreshToken:     cfg.Grafana.OAuthRefreshToken,
			ExpiresAt:        expiresAt,
			RefreshExpiresAt: refreshExpiresAt,
		}
		rcfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			oauthTransport.Base = rt
			return oauthTransport
		}
	case cfg.Grafana.APIToken != "":
		rcfg.BearerToken = cfg.Grafana.APIToken
	case cfg.Grafana.User != "":
		rcfg.Username = cfg.Grafana.User
		rcfg.Password = cfg.Grafana.Password
	}

	// Namespace
	var namespace string

	discoveredStackID, err := DiscoverStackID(ctx, *cfg.Grafana)

	if err == nil {
		// even if cfg.Grafana.OrgID was set - we ignore it, discoveredStackID takes precedent
		namespace = authlib.CloudNamespaceFormatter(discoveredStackID)
	} else {
		if cfg.Grafana.OrgID != 0 {
			namespace = authlib.OrgNamespaceFormatter(cfg.Grafana.OrgID)
		} else {
			namespace = authlib.CloudNamespaceFormatter(cfg.Grafana.StackID)
		}
	}

	// Wrap transport with debug logging so `-vvv` shows every HTTP request.
	// When --log-http-payload is set, also add full request/response body dumps.
	// Outermost layer: retry for rate limiting (429) and transient errors.
	prevWrap := rcfg.WrapTransport
	payloadLogging := httputils.PayloadLogging(ctx)
	rcfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if prevWrap != nil {
			rt = prevWrap(rt)
		}
		rt = &httputils.LoggingRoundTripper{Base: rt}
		if payloadLogging {
			rt = &httputils.RequestResponseLoggingRoundTripper{DecoratedTransport: rt}
		}
		return &retry.Transport{Base: rt}
	}

	return NamespacedRESTConfig{
		Config:         rcfg,
		Namespace:      namespace,
		GrafanaURL:     strings.TrimSuffix(cfg.Grafana.Server, "/"),
		oauthTransport: oauthTransport,
	}
}
