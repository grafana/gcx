package config

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
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

// WireTokenPersistence registers callbacks that cross-process-lock the config
// file, reload it so concurrent gcx invocations don't both consume the same
// rotating refresh token, and write rotated tokens back after a successful
// refresh. No-op if the config is not using OAuth proxy mode.
//
// Tokens live on stack entries and stack entries are atomic across config
// layers, so persistence targets the layer that owns the effective stack
// entry — writing a partial oauth-only entry into another layer would shadow
// the owning layer's entry wholesale. stackName is the context's stack ref;
// when empty it defaults to contextName (the stack-named-after-context
// convention used by login).
func (n *NamespacedRESTConfig) WireTokenPersistence(ctx context.Context, source Source, contextName, stackName string, sources []ConfigSource) {
	if n.oauthTransport == nil {
		return
	}
	if stackName == "" {
		stackName = contextName
	}
	persistSource := ResolveTokenPersistenceSource(ctx, source, stackName, sources)
	// Persistence runs inside an HTTP RoundTrip whose request context may be
	// cancelled the moment the caller has what it needs. Use a context
	// detached from that cancellation so Load/Write always complete.
	persistCtx := context.WithoutCancel(ctx)

	n.oauthTransport.Lock = func(reqCtx context.Context) (func(), error) {
		path, err := persistSource()
		if err != nil {
			return nil, err
		}
		lock := flock.New(path + ".lock")
		lockCtx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), 30*time.Second)
		defer cancel()
		if ok, err := lock.TryLockContext(lockCtx, 100*time.Millisecond); err != nil || !ok {
			return nil, err
		}
		return func() { _ = lock.Unlock() }, nil
	}

	n.oauthTransport.Reload = func() (auth.StoredTokens, bool, error) {
		fresh, err := Load(persistCtx, persistSource)
		if err != nil {
			return auth.StoredTokens{}, false, err
		}
		s := fresh.Stacks[stackName]
		if s == nil || s.Grafana == nil || s.Grafana.OAuthRefreshToken == "" {
			return auth.StoredTokens{}, false, nil
		}
		return auth.StoredTokens{
			Token:            s.Grafana.OAuthToken,
			RefreshToken:     s.Grafana.OAuthRefreshToken,
			ExpiresAt:        parseRFC3339OrZero(s.Grafana.OAuthTokenExpiresAt),
			RefreshExpiresAt: parseRFC3339OrZero(s.Grafana.OAuthRefreshExpiresAt),
		}, true, nil
	}

	n.SetOnRefresh(func(token, refreshToken, expiresAt, refreshExpiresAt string) error {
		fresh, err := Load(persistCtx, persistSource)
		if err != nil {
			return err
		}

		if fresh.Stacks[stackName] == nil {
			fresh.SetStack(stackName, StackConfig{})
		}
		s := fresh.Stacks[stackName]
		if s.Grafana == nil {
			s.Grafana = &GrafanaConfig{}
			fresh.Resolve()
		}

		g := s.Grafana
		g.OAuthToken = token
		g.OAuthRefreshToken = refreshToken
		g.OAuthTokenExpiresAt = expiresAt
		g.OAuthRefreshExpiresAt = refreshExpiresAt
		return Write(persistCtx, persistSource, fresh)
	})
}

// ResolveTokenPersistenceSource picks the best config file to persist rotated
// OAuth tokens. It returns a Source pointing to the highest-priority file
// whose stacks map defines an entry with OAuth fields for the given stack,
// then the highest file defining the stack at all, falling back to the
// user-level config or the provided fallback. Matching on the stack (not the
// context) keeps writes in the layer that owns the effective entry: stack
// entries are atomic across layers, so a partial entry written elsewhere
// would shadow the owner wholesale.
func ResolveTokenPersistenceSource(ctx context.Context, fallback Source, stackName string, sources []ConfigSource) Source {
	if len(sources) == 0 {
		return fallback
	}

	// Explicit mode bypasses layered config and should always persist to the explicit file.
	for _, src := range sources {
		if src.Type == "explicit" {
			return ExplicitConfigFile(src.Path)
		}
	}

	if src, ok := pickHighestSourceForStack(ctx, sources, stackName, stackHasOAuthFields); ok {
		return ExplicitConfigFile(src.Path)
	}
	if src, ok := pickHighestSourceForStack(ctx, sources, stackName, stackExists); ok {
		return ExplicitConfigFile(src.Path)
	}

	// No source has the stack; default to user layer when available.
	for _, src := range slices.Backward(sources) {
		if src.Type == "user" {
			return ExplicitConfigFile(src.Path)
		}
	}

	return fallback
}

func pickHighestSourceForStack(ctx context.Context, sources []ConfigSource, stackName string, match func(*StackConfig) bool) (ConfigSource, bool) {
	// DiscoverSources returns low→high precedence, so scan in reverse.
	for _, src := range slices.Backward(sources) {
		cfg, err := Load(ctx, ExplicitConfigFile(src.Path))
		if err != nil {
			continue
		}
		if s := cfg.Stacks[stackName]; s != nil && match(s) {
			return src, true
		}
	}
	return ConfigSource{}, false
}

func stackExists(s *StackConfig) bool {
	return s != nil
}

func stackHasOAuthFields(s *StackConfig) bool {
	if s == nil || s.Grafana == nil {
		return false
	}
	g := s.Grafana
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
func NewNamespacedRESTConfig(ctx context.Context, cfg Context) (NamespacedRESTConfig, error) {
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
		// Resolve file paths to data before passing to the k8s REST client.
		if err := cfg.Grafana.TLS.ResolveFiles(); err != nil {
			return NamespacedRESTConfig{}, fmt.Errorf("TLS configuration: %w", err)
		}
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
	namespace := resolveNamespace(ctx, *cfg.Grafana)

	// Wrap transport with debug logging so `-vvv` shows every HTTP request.
	// When --insecure-log-http-payload is set, also add full request/response body dumps.
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
		rt = &retry.Transport{Base: rt}
		// Outermost layer: stamp the caller-id header so every datasource query
		// (unified query API and legacy proxy alike) is attributable upstream,
		// and so it's visible to the logging transports above.
		return &httputils.CallerIDTransport{Base: rt}
	}

	return NamespacedRESTConfig{
		Config:         rcfg,
		Namespace:      namespace,
		GrafanaURL:     strings.TrimSuffix(cfg.Grafana.Server, "/"),
		oauthTransport: oauthTransport,
	}, nil
}
