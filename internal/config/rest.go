package config

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/credentials"
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

	// oauthCredentialBinding freezes the source/owner/field/destination tuple
	// that authorized the OAuth transport. Persistence compares a freshly
	// loaded stack against it before resolving or writing rotated credentials,
	// so a concurrent server, proxy, or TLS trust change cannot adopt them.
	oauthCredentialBinding credentials.Binding
}

// IsOAuthProxy reports whether the config is using OAuth proxy mode.
func (n *NamespacedRESTConfig) IsOAuthProxy() bool {
	return n.oauthTransport != nil
}

// FreshOAuthToken returns a usable access token through the configured OAuth
// refresh lifecycle. Callers that need a token outside an HTTP RoundTrip (for
// example A2A streaming) should use this instead of implementing a separate
// refresh path.
func (n *NamespacedRESTConfig) FreshOAuthToken(ctx context.Context) (string, error) {
	if n.oauthTransport == nil {
		return "", errors.New("OAuth proxy transport is not configured")
	}
	return n.oauthTransport.FreshToken(ctx)
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
//
//nolint:gocyclo // Refresh locking, reload, binding CAS, and persistence form one security-critical transaction.
func (n *NamespacedRESTConfig) WireTokenPersistence(ctx context.Context, source Source, contextName, stackName string, sources []ConfigSource) {
	if n.oauthTransport == nil {
		return
	}
	if stackName == "" {
		stackName = contextName
	}
	persistSource := ResolveTokenPersistenceSource(ctx, source, stackName, sources)
	expectedBinding := n.oauthCredentialBinding
	// Persistence runs inside an HTTP RoundTrip whose request context may be
	// cancelled the moment the caller has what it needs. Use a context
	// detached from that cancellation so Load/Write always complete.
	persistCtx := withConfigWriteLockHeld(context.WithoutCancel(ctx))

	persistLoad := func() (Config, error) {
		path, err := persistSource()
		if err != nil {
			return Config{}, err
		}
		loadCtx := persistCtx
		if selected, ok := configSourceForPath(sources, path); ok {
			loadCtx = withConfigLayer(loadCtx, selected.Type)
			current, readErr := readConfigSource(selected)
			if readErr != nil {
				return Config{}, readErr
			}
			if len(sources) > 1 && isLegacyConfig(current) {
				loadCtx = withMigrationPersistenceSuppressed(loadCtx)
			}
		}
		fresh, err := Load(loadCtx, persistSource)
		if err != nil {
			return fresh, err
		}
		if fresh.migrationDeferred {
			return Config{}, fmt.Errorf("OAuth token persistence requires migrating config layer %s first; load or edit that layer explicitly before retrying", path)
		}
		stack := fresh.Stacks[stackName]
		if stack == nil || stack.Grafana == nil {
			return Config{}, fmt.Errorf("OAuth credential owner %q disappeared from %s; reload configuration before retrying", stackName, path)
		}
		freshBinding := stackOwner(stackName, stack).binding(credentials.FieldOAuthRefreshToken)
		bindingChanged := freshBinding.Destination != expectedBinding.Destination
		if expectedBinding.Valid() {
			bindingChanged = freshBinding != expectedBinding
		}
		if bindingChanged {
			return Config{}, fmt.Errorf("OAuth credential destination for %q changed in %s; reload configuration before retrying", stackName, path)
		}
		if fresh.keychainStore != nil {
			backed, preserve, states := resolveSentinelsForOwner(stackOwner(stackName, stack), fresh.keychainStore)
			fresh.trackKeychainResults(backed, preserve, states)
		}
		return fresh, nil
	}

	n.oauthTransport.Lock = func(reqCtx context.Context) (func(), error) {
		path, err := persistSource()
		if err != nil {
			return nil, err
		}
		layer := ""
		if selected, ok := configSourceForPath(sources, path); ok {
			layer = selected.Type
		}
		identity, err := canonicalConfigSourceForLayer(path, layer)
		if err != nil {
			return nil, err
		}
		lockPath, err := configLockFile(identity, "write")
		if err != nil {
			return nil, err
		}
		lock := flock.New(lockPath)
		lockCtx, cancel := context.WithTimeout(reqCtx, 30*time.Second)
		defer cancel()
		ok, err := lock.TryLockContext(lockCtx, 100*time.Millisecond)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("timed out locking OAuth token persistence for %s", path)
		}
		return func() { _ = lock.Unlock() }, nil
	}

	n.oauthTransport.Reload = func() (auth.StoredTokens, bool, error) {
		fresh, err := persistLoad()
		if err != nil {
			return auth.StoredTokens{}, false, err
		}
		s := fresh.Stacks[stackName]
		if s == nil || s.Grafana == nil || s.Grafana.OAuthRefreshToken == "" {
			return auth.StoredTokens{}, false, fmt.Errorf("OAuth refresh credential %q is missing from its persistence source", stackName)
		}
		return auth.StoredTokens{
			Token:            s.Grafana.OAuthToken,
			RefreshToken:     s.Grafana.OAuthRefreshToken,
			ExpiresAt:        parseRFC3339OrZero(s.Grafana.OAuthTokenExpiresAt),
			RefreshExpiresAt: parseRFC3339OrZero(s.Grafana.OAuthRefreshExpiresAt),
		}, true, nil
	}

	n.SetOnRefresh(func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
		fresh, err := persistLoad()
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
		if g.OAuthToken == token &&
			g.OAuthRefreshToken == refreshToken &&
			g.OAuthTokenExpiresAt == expiresAt &&
			g.OAuthRefreshExpiresAt == refreshExpiresAt {
			// A prior write may have committed before reporting a durability
			// error. Treat the exact on-disk generation as idempotent success.
			return nil
		}
		refreshStateKey := stackOwner(stackName, s).stateKey(credentials.FieldOAuthRefreshToken)
		refreshState, hasRefreshState := fresh.keychainStates[refreshStateKey]
		if hasRefreshState {
			var resolutionErr error
			switch refreshState.status {
			case keychainStateUnresolved, keychainStatePreserved:
				resolutionErr = credentials.ErrUnavailable
			case keychainStateMissing:
				resolutionErr = credentials.ErrNotFound
			}
			if resolutionErr != nil {
				// The file still contains the same bound sentinel, but its plaintext
				// value could not be read. This is not evidence that another login won
				// the generation CAS. Keep the rotated generation pending so a later
				// call can retry after the keychain entry is available again.
				return fmt.Errorf("cannot verify OAuth refresh-token generation for %q: %w", stackName, resolutionErr)
			}
		}
		if g.OAuthRefreshToken != previousRefreshToken {
			return fmt.Errorf("%w for OAuth credential owner %q; reload configuration before retrying", auth.ErrTokenGenerationChanged, stackName)
		}
		g.OAuthToken = token
		g.OAuthRefreshToken = refreshToken
		g.OAuthTokenExpiresAt = expiresAt
		g.OAuthRefreshExpiresAt = refreshExpiresAt
		return Write(persistCtx, persistSource, fresh)
	})
}

func configSourceForPath(sources []ConfigSource, path string) (ConfigSource, bool) {
	for _, source := range sources {
		if source.Path == path {
			return source, true
		}
	}
	return ConfigSource{}, false
}

// ResolveTokenPersistenceSource picks the best config file to persist rotated
// OAuth tokens. It returns a Source pointing to the highest-priority file
// whose stacks map defines the given stack, falling back to the user-level
// config or the provided fallback. Stack entries are atomic across layers, so
// the highest defining layer owns the effective entry even when only a lower
// shadowed layer contains OAuth fields.
func ResolveTokenPersistenceSource(ctx context.Context, fallback Source, stackName string, sources []ConfigSource) Source {
	resolved, err := resolveTokenPersistenceSource(ctx, fallback, stackName, sources)
	if err != nil {
		return func() (string, error) { return "", err }
	}
	return resolved
}

func resolveTokenPersistenceSource(ctx context.Context, fallback Source, stackName string, sources []ConfigSource) (Source, error) {
	if len(sources) == 0 {
		return fallback, nil
	}

	// Explicit mode bypasses layered config and should always persist to the explicit file.
	for _, src := range sources {
		if src.Type == "explicit" {
			return ExplicitConfigFile(src.Path), nil
		}
	}

	if src, ok, err := pickHighestSourceForStack(ctx, sources, stackName, stackExists); err != nil {
		return nil, err
	} else if ok {
		return ExplicitConfigFile(src.Path), nil
	}

	// No source has the stack; default to user layer when available.
	for _, src := range slices.Backward(sources) {
		if src.Type == "user" {
			return ExplicitConfigFile(src.Path), nil
		}
	}

	return fallback, nil
}

func pickHighestSourceForStack(ctx context.Context, sources []ConfigSource, stackName string, match func(*StackConfig) bool) (ConfigSource, bool, error) {
	// DiscoverSources returns low→high precedence, so scan in reverse.
	for _, src := range slices.Backward(sources) {
		loadCtx := withMigrationPersistenceSuppressed(withConfigLayer(ctx, src.Type))
		cfg, err := Load(loadCtx, ExplicitConfigFile(src.Path))
		if err != nil {
			return ConfigSource{}, false, fmt.Errorf("rescan OAuth persistence source %s: %w", src.Path, err)
		}
		if s := cfg.Stacks[stackName]; s != nil && match(s) {
			return src, true, nil
		}
	}
	return ConfigSource{}, false, nil
}

func stackExists(s *StackConfig) bool {
	return s != nil
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
	if cfg.Grafana == nil {
		return NamespacedRESTConfig{}, ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'", cfg.Name),
			Message: "context references no stack with grafana config",
		}
	}
	authSelection, err := cfg.validatedGrafanaAuthSelection()
	if err != nil {
		return NamespacedRESTConfig{}, err
	}
	selectedGrafana := *cfg.Grafana
	selectedGrafana.TLS = cfg.Grafana.tlsForSelectedAuth(authSelection)

	rcfg := rest.Config{
		UserAgent:       version.UserAgent(),
		Host:            strings.TrimSuffix(cfg.Grafana.Server, "/"),
		APIPath:         "/apis",
		TLSClientConfig: rest.TLSClientConfig{},
		// TODO: make configurable
		QPS:   50,
		Burst: 100,
	}

	if selectedGrafana.TLS != nil {
		resolvedTLS := *selectedGrafana.TLS
		// Resolve file paths to data before passing to the k8s REST client.
		if err := resolvedTLS.ResolveFiles(); err != nil {
			return NamespacedRESTConfig{}, fmt.Errorf("TLS configuration: %w", err)
		}
		// Kubernetes really is wonderful, huh.
		// tl;dr it has its own TLSClientConfig,
		// and it's not compatible with the one from the "crypto/tls" package.
		rcfg.TLSClientConfig = rest.TLSClientConfig{
			Insecure:   resolvedTLS.Insecure,
			ServerName: resolvedTLS.ServerName,
			CertData:   resolvedTLS.CertData,
			KeyData:    resolvedTLS.KeyData,
			CAData:     resolvedTLS.CAData,
			NextProtos: resolvedTLS.NextProtos,
		}
	}

	// Authentication
	var oauthTransport *auth.RefreshTransport
	var oauthCredentialBinding credentials.Binding
	switch authSelection.mode {
	case grafanaAuthOAuth:
		// OAuth proxy mode: route requests through the assistant backend proxy.
		// The ProxyEndpoint may differ from Server (e.g. cloud routing through
		// the assistant backend), so it is stored as a separate config field.
		// RefreshTransport handles bearer auth and token renewal; no BearerToken
		// on rcfg to avoid client-go adding a redundant auth layer.
		rcfg.Host = strings.TrimSuffix(cfg.Grafana.ProxyEndpoint, "/") + "/api/cli/v1/proxy"

		// A zero expiry with a refresh token triggers renewal on first request.
		// Access-only OAuth credentials with unknown expiry remain usable until
		// the server rejects them, because there is no refresh path available.
		expiresAt := parseRFC3339OrZero(cfg.Grafana.OAuthTokenExpiresAt)
		refreshExpiresAt := parseRFC3339OrZero(cfg.Grafana.OAuthRefreshExpiresAt)
		oauthTransport = &auth.RefreshTransport{
			ProxyEndpoint:    cfg.Grafana.ProxyEndpoint,
			Token:            cfg.Grafana.OAuthToken,
			RefreshToken:     cfg.Grafana.OAuthRefreshToken,
			ExpiresAt:        expiresAt,
			RefreshExpiresAt: refreshExpiresAt,
		}
		bindingStack := cfg.StackEntry
		if bindingStack == nil {
			bindingStack = &StackConfig{Grafana: cfg.Grafana}
		}
		oauthCredentialBinding = stackOwner(cfg.stackName(), bindingStack).binding(credentials.FieldOAuthRefreshToken)
		rcfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			oauthTransport.Base = rt
			return oauthTransport
		}
	case grafanaAuthToken:
		rcfg.BearerToken = cfg.Grafana.APIToken
	case grafanaAuthBasic:
		rcfg.Username = cfg.Grafana.User
		rcfg.Password = cfg.Grafana.Password
	}

	// Namespace
	namespace := resolveNamespace(ctx, selectedGrafana)

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
		Config:                 rcfg,
		Namespace:              namespace,
		GrafanaURL:             strings.TrimSuffix(cfg.Grafana.Server, "/"),
		oauthTransport:         oauthTransport,
		oauthCredentialBinding: oauthCredentialBinding,
	}, nil
}
