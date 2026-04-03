package config

import (
	"context"
	"net/http"
	"strings"
	"time"

	authlib "github.com/grafana/authlib/types"
	"github.com/grafana/gcx/internal/auth"
	"k8s.io/client-go/rest"
)

// NamespacedRESTConfig is a REST config with a namespace.
// TODO: move to app SDK?
type NamespacedRESTConfig struct {
	rest.Config

	Namespace string

	// oauthTransport holds a reference to the RefreshTransport when OAuth proxy
	// mode is active, allowing callers to wire the OnRefresh callback after
	// construction (Option C: call-site wiring).
	oauthTransport *auth.RefreshTransport
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
// from source, updates the OAuth token fields for contextName, and writes back.
// No-op if the config is not using OAuth proxy mode.
func (n *NamespacedRESTConfig) WireTokenPersistence(ctx context.Context, source Source, contextName string) {
	n.SetOnRefresh(func(token, refreshToken, expiresAt, refreshExpiresAt string) error {
		fresh, err := Load(ctx, source)
		if err != nil {
			return err
		}
		c := fresh.Contexts[contextName]
		if c == nil || c.Grafana == nil {
			return nil
		}
		c.Grafana.OAuthToken = token
		c.Grafana.OAuthRefreshToken = refreshToken
		c.Grafana.OAuthTokenExpiresAt = expiresAt
		c.Grafana.OAuthRefreshExpiresAt = refreshExpiresAt
		return Write(ctx, source, fresh)
	})
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
		// TODO add user agent
		// UserAgent: cfg.UserAgent.ValueString(),
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

	return NamespacedRESTConfig{
		Config:         rcfg,
		Namespace:      namespace,
		oauthTransport: oauthTransport,
	}
}
