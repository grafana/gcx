package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testOIDCClientID = "gcx-test-client"

// fakeIDP is a minimal OIDC provider: discovery, an authorize URL that is
// never fetched, and a token endpoint that records its forms.
type fakeIDP struct {
	server *httptest.Server
	issuer string

	mu            sync.Mutex
	forms         []url.Values
	idTokenClaims map[string]any
	refreshToken  string
	tokenStatus   int
	tokenError    string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	idp := &fakeIDP{tokenStatus: http.StatusOK, refreshToken: "refresh-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.issuer,
			"authorization_endpoint": idp.issuer + "/authorize",
			"token_endpoint":         idp.issuer + "/token",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		idp.mu.Lock()
		defer idp.mu.Unlock()
		idp.forms = append(idp.forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		if idp.tokenStatus != http.StatusOK {
			w.WriteHeader(idp.tokenStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": idp.tokenError})
			return
		}
		resp := map[string]any{"id_token": fakeJWT(idp.idTokenClaims), "access_token": "unused"}
		if idp.refreshToken != "" {
			resp["refresh_token"] = idp.refreshToken
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	idp.issuer = idp.server.URL + "/tenant/v2.0"
	// Serve discovery under the issuer path as well.
	mux.HandleFunc("/tenant/v2.0/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.issuer,
			"authorization_endpoint": idp.server.URL + "/authorize",
			"token_endpoint":         idp.server.URL + "/token",
		})
	})
	idp.idTokenClaims = idp.claims("")
	return idp
}

func (idp *fakeIDP) claims(nonce string) map[string]any {
	c := map[string]any{
		"iss":   idp.issuer,
		"aud":   testOIDCClientID,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "jane@example.com",
	}
	if nonce != "" {
		c["nonce"] = nonce
	}
	return c
}

func (idp *fakeIDP) setClaims(c map[string]any) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.idTokenClaims = c
}

func (idp *fakeIDP) lastForm() url.Values {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	if len(idp.forms) == 0 {
		return nil
	}
	return idp.forms[len(idp.forms)-1]
}

func fakeJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// authURLWriter captures flow output and reports the printed authorize URL.
type authURLWriter struct {
	mu   sync.Mutex
	buf  strings.Builder
	once sync.Once
	urls chan string
}

var authorizeURLPattern = regexp.MustCompile(`http://127\.0\.0\.1:\d+/authorize\?\S+`)

func (w *authURLWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if m := authorizeURLPattern.FindString(w.buf.String()); m != "" {
		w.once.Do(func() { w.urls <- m })
	}
	return len(p), nil
}

// runManualOIDC runs the manual flow and answers it with a redirect URL that
// carries the authorize request's state. mutate may change the ID token
// claims after the nonce is known.
func runManualOIDC(t *testing.T, idp *fakeIDP, mutate func(nonce string) map[string]any) (*auth.OIDCResult, url.Values, error) {
	t.Helper()
	writer := &authURLWriter{urls: make(chan string, 1)}
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	var authQuery url.Values
	go func() {
		authURL := <-writer.urls
		u, err := url.Parse(authURL)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		authQuery = u.Query()
		idp.setClaims(mutate(authQuery.Get("nonce")))
		redirect := fmt.Sprintf("%s?code=the-code&state=%s", authQuery.Get("redirect_uri"), url.QueryEscape(authQuery.Get("state")))
		_, _ = io.WriteString(pw, redirect+"\n")
		// A rejected paste re-prompts; end the input so the flow reports it.
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := auth.NewOIDCFlow(auth.OIDCOptions{
		Issuer:   idp.issuer,
		ClientID: testOIDCClientID,
		Writer:   writer,
		Reader:   pr,
		Manual:   true,
	}).Run(ctx)
	return result, authQuery, err
}

func TestOIDCFlow_ManualLogin(t *testing.T) {
	idp := newFakeIDP(t)
	result, authQuery, err := runManualOIDC(t, idp, idp.claims)
	require.NoError(t, err)

	assert.Equal(t, "jane@example.com", result.Email)
	assert.Equal(t, "refresh-1", result.RefreshToken)
	assert.NotEmpty(t, result.IDToken)
	assert.NotEmpty(t, result.ExpiresAt)

	assert.Equal(t, "code", authQuery.Get("response_type"))
	assert.Equal(t, testOIDCClientID, authQuery.Get("client_id"))
	assert.Equal(t, "S256", authQuery.Get("code_challenge_method"))
	assert.Equal(t, "openid profile email offline_access", authQuery.Get("scope"))
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/callback", auth.ManualCallbackPort), authQuery.Get("redirect_uri"))

	form := idp.lastForm()
	assert.Equal(t, "authorization_code", form.Get("grant_type"))
	assert.Equal(t, "the-code", form.Get("code"))
	assert.Equal(t, authQuery.Get("redirect_uri"), form.Get("redirect_uri"))
	assert.NotEmpty(t, form.Get("code_verifier"))
}

func TestOIDCFlow_RejectsInvalidIDToken(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c map[string]any)
		wantErr string
	}{
		{"nonce mismatch", func(c map[string]any) { c["nonce"] = "other" }, "nonce mismatch"},
		{"foreign audience", func(c map[string]any) { c["aud"] = []string{"someone-else"} }, "audience"},
		{"foreign issuer", func(c map[string]any) { c["iss"] = "https://evil.example.com" }, "issuer"},
		{"expired", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }, "expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := newFakeIDP(t)
			_, _, err := runManualOIDC(t, idp, func(nonce string) map[string]any {
				c := idp.claims(nonce)
				tt.mutate(c)
				return c
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDiscoverOIDC_IssuerMismatch(t *testing.T) {
	idp := newFakeIDP(t)
	// The root discovery document names the /tenant/v2.0 issuer.
	_, err := auth.DiscoverOIDC(context.Background(), idp.server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestDiscoverOIDC_RequiresHTTPS(t *testing.T) {
	_, err := auth.DiscoverOIDC(context.Background(), "http://idp.example.com/tenant")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTPS")
}

func TestOIDCRefresh(t *testing.T) {
	t.Run("keeps refresh token when the provider does not rotate it", func(t *testing.T) {
		idp := newFakeIDP(t)
		idp.refreshToken = ""
		result, err := auth.OIDCRefresh(context.Background(), idp.issuer, testOIDCClientID, nil, "refresh-0")
		require.NoError(t, err)
		assert.Equal(t, "refresh-0", result.RefreshToken)
		assert.NotEmpty(t, result.Token)
		assert.NotEmpty(t, result.ExpiresAt)

		form := idp.lastForm()
		assert.Equal(t, "refresh_token", form.Get("grant_type"))
		assert.Equal(t, "refresh-0", form.Get("refresh_token"))
		assert.Equal(t, testOIDCClientID, form.Get("client_id"))
	})

	t.Run("adopts a rotated refresh token", func(t *testing.T) {
		idp := newFakeIDP(t)
		idp.refreshToken = "refresh-2"
		result, err := auth.OIDCRefresh(context.Background(), idp.issuer, testOIDCClientID, nil, "refresh-1")
		require.NoError(t, err)
		assert.Equal(t, "refresh-2", result.RefreshToken)
	})

	t.Run("invalid_grant means the refresh token expired", func(t *testing.T) {
		idp := newFakeIDP(t)
		idp.tokenStatus = http.StatusBadRequest
		idp.tokenError = "invalid_grant"
		_, err := auth.OIDCRefresh(context.Background(), idp.issuer, testOIDCClientID, nil, "refresh-1")
		require.ErrorIs(t, err, auth.ErrRefreshTokenExpired)
	})

	t.Run("an ID token from another issuer is an invalid refresh response", func(t *testing.T) {
		idp := newFakeIDP(t)
		idp.setClaims(map[string]any{"iss": "https://other.example.com", "aud": testOIDCClientID})
		_, err := auth.OIDCRefresh(context.Background(), idp.issuer, testOIDCClientID, nil, "refresh-1")
		require.ErrorIs(t, err, auth.ErrInvalidRefreshResponse)
	})
}

func TestRefreshTransport_UsesCustomRefresh(t *testing.T) {
	var gotAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	var refreshedWith string
	transport := &auth.RefreshTransport{
		Token:        "expired-id-token",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Refresh: func(_ context.Context, refreshToken string) (auth.RefreshResult, error) {
			refreshedWith = refreshToken
			return auth.RefreshResult{Token: "new-id-token", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, nil
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, backend.URL, nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "refresh-1", refreshedWith)
	assert.Equal(t, "Bearer new-id-token", gotAuth)
}

func TestOIDCFlow_ReportsProviderErrorDescription(t *testing.T) {
	idp := newFakeIDP(t)
	writer := &authURLWriter{urls: make(chan string, 1)}
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	go func() {
		u, _ := url.Parse(<-writer.urls)
		q := u.Query()
		_, _ = io.WriteString(pw, fmt.Sprintf("%s?error=invalid_request&error_description=%s&state=%s\n",
			q.Get("redirect_uri"), url.QueryEscape("AADSTS50011: redirect URI mismatch"), url.QueryEscape(q.Get("state"))))
		_ = pw.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := auth.NewOIDCFlow(auth.OIDCOptions{
		Issuer: idp.issuer, ClientID: testOIDCClientID, Writer: writer, Reader: pr, Manual: true,
	}).Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AADSTS50011")
}
