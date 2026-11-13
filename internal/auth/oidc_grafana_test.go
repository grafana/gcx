package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDiscoveryIDP serves one OIDC discovery document at issuerPath that names
// authorizePath as its authorize endpoint.
func newDiscoveryIDP(t *testing.T, issuerPath, authorizePath string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != issuerPath+"/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL + issuerPath,
			"authorization_endpoint": srv.URL + authorizePath,
			"token_endpoint":         srv.URL + issuerPath + "/token",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newFakeGrafana lists provider in /bootdata, redirects /login/<provider> to
// target and every other login route back to its own /login page.
func newFakeGrafana(t *testing.T, provider, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]any{
				"oauth": map[string]any{provider: map[string]string{"name": "Company SSO"}, "github": map[string]string{"name": "GitHub"}},
			}})
		case "/login/" + provider:
			http.Redirect(w, r, target, http.StatusFound)
		case "/login":
			w.WriteHeader(http.StatusOK)
		default:
			http.Redirect(w, r, "/login", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverGrafanaOIDC(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		issuerPath    string
		authorizePath string
	}{
		{"Entra ID", "azuread", "/tenant-id/v2.0", "/tenant-id/oauth2/v2.0/authorize"},
		{"Keycloak", "generic_oauth", "/realms/main", "/realms/main/protocol/openid-connect/auth"},
		{"Okta", "okta", "/oauth2/default", "/oauth2/default/v1/authorize"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := newDiscoveryIDP(t, tt.issuerPath, tt.authorizePath)
			grafana := newFakeGrafana(t, tt.provider, idp.URL+tt.authorizePath+"?client_id=the-client&scope=openid+email+profile&state=x")

			got, err := auth.DiscoverGrafanaOIDC(context.Background(), grafana.URL, "", nil)
			require.NoError(t, err)
			assert.Equal(t, tt.provider, got.Provider)
			assert.Equal(t, idp.URL+tt.issuerPath, got.Issuer)
			assert.Equal(t, "the-client", got.ClientID)
			assert.Equal(t, []string{"openid", "offline_access", "email", "profile"}, got.Scopes)
		})
	}
}

func TestDiscoverGrafanaOIDC_NoSSO(t *testing.T) {
	grafana := newFakeGrafana(t, "none", "")
	_, err := auth.DiscoverGrafanaOIDC(context.Background(), grafana.URL, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not redirect any SSO login")
}

func TestDiscoverGrafanaOIDC_RejectsForeignAuthorizeEndpoint(t *testing.T) {
	// The discovery document exists but names another authorize endpoint, so
	// it cannot be the provider Grafana redirects to.
	idp := newDiscoveryIDP(t, "/tenant-id/v2.0", "/somewhere/else")
	grafana := newFakeGrafana(t, "azuread", idp.URL+"/tenant-id/oauth2/v2.0/authorize?client_id=c")

	_, err := auth.DiscoverGrafanaOIDC(context.Background(), grafana.URL, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no OIDC discovery document")
}

func TestNormalizeOIDCScopes(t *testing.T) {
	assert.Equal(t, []string{"openid", "offline_access"}, auth.NormalizeOIDCScopes(nil))
	assert.Equal(t, []string{"openid", "offline_access", "email", "profile"},
		auth.NormalizeOIDCScopes([]string{"email", "openid", "profile", "email", ""}))
}

func TestGrafanaSSOLogins(t *testing.T) {
	grafana := newFakeGrafana(t, "azuread", "https://idp.example.com/authorize")
	logins, err := auth.GrafanaSSOLogins(context.Background(), grafana.URL, nil)
	require.NoError(t, err)
	// GitHub is listed by Grafana but is not an OIDC provider gcx can use.
	assert.Equal(t, []auth.GrafanaSSOLogin{{ID: "azuread", Name: "Company SSO"}}, logins)
}

func TestDiscoverGrafanaOIDC_ProbesOnlyListedOrChosenLogins(t *testing.T) {
	var probed []string
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]any{
				"oauth": map[string]any{"okta": map[string]string{"name": "Okta"}},
			}})
		default:
			probed = append(probed, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(grafana.Close)

	_, err := auth.DiscoverGrafanaOIDC(context.Background(), grafana.URL, "", nil)
	require.ErrorIs(t, err, auth.ErrGrafanaOIDCNotFound)
	assert.Equal(t, []string{"/login/okta"}, probed, "only the logins /bootdata lists")

	probed = nil
	_, err = auth.DiscoverGrafanaOIDC(context.Background(), grafana.URL, "generic_oauth", nil)
	require.ErrorIs(t, err, auth.ErrGrafanaOIDCNotFound)
	assert.Equal(t, []string{"/login/generic_oauth"}, probed, "only the chosen login")
}
