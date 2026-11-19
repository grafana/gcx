package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/httputils"
)

// grafanaOAuthProviders are the Grafana SSO login routes that front an OIDC
// identity provider supporting discovery and offline_access, in the order
// they are tried. generic_oauth covers providers such as Keycloak. Grafana's
// github and grafana_com logins are not OIDC; google and gitlab need
// provider-specific refresh handling and are left out.
var grafanaOAuthProviders = []string{"azuread", "generic_oauth", "okta"} //nolint:gochecknoglobals

// GrafanaSSOLogin is an SSO login that a Grafana server offers.
type GrafanaSSOLogin struct {
	// ID is the login route name, e.g. "azuread".
	ID string
	// Name is the display name of Grafana's login button.
	Name string
}

// GrafanaSSOLogins lists the SSO logins that the Grafana server offers and
// that gcx can use, read from the unauthenticated /bootdata endpoint that
// Grafana's own login page loads. client may be nil; it is used for the
// request to the Grafana server (custom CA, mTLS).
func GrafanaSSOLogins(ctx context.Context, server string, client *http.Client) ([]GrafanaSSOLogin, error) {
	server = strings.TrimSuffix(strings.TrimSpace(server), "/")
	if err := validateOIDCURL(server); err != nil {
		return nil, fmt.Errorf("invalid Grafana server: %w", err)
	}
	if client == nil {
		client = httputils.NewClient(httputils.ClientOpts{DisableRetry: true})
	}
	ctx, cancel := context.WithTimeout(ctx, grafanaOIDCRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/bootdata", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read Grafana login settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read Grafana login settings: status %d", resp.StatusCode)
	}
	var bootdata struct {
		Settings struct {
			OAuth map[string]struct {
				Name string `json:"name"`
			} `json:"oauth"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&bootdata); err != nil {
		return nil, fmt.Errorf("read Grafana login settings: %w", err)
	}
	var logins []GrafanaSSOLogin
	for _, id := range grafanaOAuthProviders {
		provider, ok := bootdata.Settings.OAuth[id]
		if !ok {
			continue
		}
		name := StripControlChars(strings.TrimSpace(provider.Name))
		if name == "" {
			name = id
		}
		logins = append(logins, GrafanaSSOLogin{ID: id, Name: name})
	}
	return logins, nil
}

// grafanaOIDCDiscoveryTimeout bounds the whole best-effort discovery,
// including every candidate issuer probe.
const grafanaOIDCDiscoveryTimeout = 15 * time.Second

// grafanaOIDCRequestTimeout bounds a single discovery request.
const grafanaOIDCRequestTimeout = 5 * time.Second

// ErrGrafanaOIDCNotFound reports that the Grafana server does not redirect any
// SSO login to an OIDC identity provider that gcx can use.
var ErrGrafanaOIDCNotFound = errors.New("grafana does not redirect any SSO login to an OIDC identity provider")

// GrafanaOIDCProvider is the identity provider behind a Grafana SSO login.
type GrafanaOIDCProvider struct {
	// Provider is the Grafana login route, e.g. "azuread".
	Provider string
	Issuer   string
	ClientID string
	// Scopes are the scopes Grafana's own login requests.
	Scopes []string
}

// DiscoverGrafanaOIDC finds the OIDC issuer, client ID and scopes that a
// Grafana server uses for its browser SSO login.
//
// This is best effort: Grafana does not publish its SSO configuration, so gcx
// requests /login/<provider> without following the redirect. The redirect to
// the provider's authorize endpoint carries the client ID and scopes. The
// issuer is the candidate URL whose OIDC discovery document names exactly that
// authorize endpoint, so a wrong guess cannot select another provider. Callers
// should offer an explicit issuer and client ID as the fallback.
//
// client may be nil; it is used only for requests to the Grafana server
// (custom CA, mTLS).
//
// provider restricts discovery to one Grafana login route, e.g. "azuread".
// Empty tries the SSO logins that /bootdata lists, or every supported route
// when /bootdata cannot be read.
func DiscoverGrafanaOIDC(ctx context.Context, server, provider string, client *http.Client) (*GrafanaOIDCProvider, error) {
	server = strings.TrimSuffix(strings.TrimSpace(server), "/")
	if err := validateOIDCURL(server); err != nil {
		return nil, fmt.Errorf("invalid Grafana server: %w", err)
	}
	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, fmt.Errorf("invalid Grafana server: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, grafanaOIDCDiscoveryTimeout)
	defer cancel()

	if client == nil {
		client = httputils.NewClient(httputils.ClientOpts{DisableRetry: true})
	}
	noRedirect := *client
	noRedirect.Timeout = grafanaOIDCRequestTimeout
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	candidates := grafanaOAuthProviders
	switch {
	case provider != "":
		candidates = []string{provider}
	default:
		if logins, err := GrafanaSSOLogins(ctx, server, client); err == nil {
			candidates = candidates[:0:0]
			for _, login := range logins {
				candidates = append(candidates, login.ID)
			}
		}
	}

	for _, route := range candidates {
		authorizeURL, err := grafanaLoginRedirect(ctx, &noRedirect, server+"/login/"+route)
		if errors.Is(err, errNoSSORedirect) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if authorizeURL.Host == serverURL.Host {
			// Disabled providers redirect back to Grafana's own login page.
			continue
		}
		clientID := authorizeURL.Query().Get("client_id")
		if clientID == "" {
			continue
		}
		issuer, err := issuerForAuthorizeEndpoint(ctx, authorizeURL)
		if err != nil {
			return nil, fmt.Errorf("grafana login %q redirects to %s: %w", route, authorizeURL.Host, err)
		}
		return &GrafanaOIDCProvider{
			Provider: route,
			Issuer:   issuer,
			ClientID: clientID,
			Scopes:   NormalizeOIDCScopes(strings.Fields(authorizeURL.Query().Get("scope"))),
		}, nil
	}
	return nil, ErrGrafanaOIDCNotFound
}

// errNoSSORedirect reports a Grafana login route that does not redirect, for
// example because the provider is not configured.
var errNoSSORedirect = errors.New("no SSO redirect")

func grafanaLoginRedirect(ctx context.Context, client *http.Client, loginURL string) (*url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe Grafana SSO login: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return nil, errNoSSORedirect
	}
	location, err := resp.Location()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoSSORedirect, err)
	}
	return location, nil
}

// issuerForAuthorizeEndpoint tries the path prefixes of the authorize
// endpoint, and each prefix with "/v2.0" appended (Entra ID), as issuers. Only
// an issuer whose discovery document names the same authorize endpoint is
// accepted.
func issuerForAuthorizeEndpoint(ctx context.Context, authorizeURL *url.URL) (string, error) {
	endpoint := authorizeURL.Scheme + "://" + authorizeURL.Host + authorizeURL.Path
	if err := validateOIDCURL(endpoint); err != nil {
		return "", err
	}
	// Guessed candidates mostly 404; retries would only spend the budget.
	client := httputils.NewClient(httputils.ClientOpts{Timeout: grafanaOIDCRequestTimeout, DisableRetry: true})
	base := authorizeURL.Scheme + "://" + authorizeURL.Host
	segments := strings.Split(strings.Trim(authorizeURL.Path, "/"), "/")
	for i := range slices.Backward(segments) {
		prefix := base
		if i > 0 {
			prefix += "/" + strings.Join(segments[:i], "/")
		}
		for _, candidate := range []string{prefix, prefix + "/v2.0"} {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			provider, err := discoverOIDC(ctx, candidate, client)
			if err != nil {
				continue
			}
			if strings.TrimSuffix(provider.AuthorizationEndpoint, "/") == strings.TrimSuffix(endpoint, "/") {
				return provider.Issuer, nil
			}
		}
	}
	return "", errors.New("no OIDC discovery document names this authorize endpoint")
}
