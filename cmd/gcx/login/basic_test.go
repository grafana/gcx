//nolint:testpackage // Exercises the login command constructor and prompt options.
package login

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicLogin(t *testing.T) {
	t.Setenv("GCX_KEYCHAIN", "off")
	t.Setenv("GRAFANA_TOKEN", "environment-token-must-not-win")
	t.Setenv("GRAFANA_USER", "environment-user")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	const password = " password-with-spaces "
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user":
			probes.Add(1)
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			if user != "admin" || pass != password {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprintf(w, `{"message":%q}`, "rejected password: "+pass)
				return
			}
			_, _ = w.Write([]byte(`{"id":1,"login":"admin"}`))
		case "/api/health":
			_, _ = w.Write([]byte(`{"version":"12.0.0"}`))
		case "/api", "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	execute := func(pass string) (string, error) {
		t.Helper()
		t.Setenv("GRAFANA_PASSWORD", pass)
		cmd := Command()
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs([]string{"local", "--config", path, "--server", server.URL, "--basic-auth", "--user", " admin ", "--yes", "-o", "json"})
		err := cmd.ExecuteContext(t.Context())
		return output.String(), err
	}
	output, err := execute(password)
	require.NoError(t, err)
	assert.NotContains(t, output, password)
	assert.Contains(t, output, `"authMethod": "basic"`)
	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	grafana := cfg.Contexts["local"].Grafana
	assert.Equal(t, "basic", grafana.AuthMethod)
	assert.Equal(t, "admin", grafana.User)
	assert.Equal(t, password, grafana.Password)
	assert.Empty(t, grafana.APIToken)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	// Prime the real discovery cache: credential checks must still hit /api/user.
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["local"])
	require.NoError(t, err)
	_, err = discovery.NewDefaultRegistry(t.Context(), restCfg)
	require.NoError(t, err)
	output, err = execute("wrong-password-secret")
	var authErr *login.BasicAuthCheckError
	require.ErrorAs(t, err, &authErr)
	assert.Equal(t, http.StatusUnauthorized, authErr.Status)
	assert.NotContains(t, err.Error()+output, "wrong-password-secret")
	assert.EqualValues(t, 2, probes.Load())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	for _, method := range []string{"token", "oauth"} {
		t.Run("switch from "+method, func(t *testing.T) {
			t.Setenv("GRAFANA_TOKEN", "")
			cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
			require.NoError(t, err)
			grafana := cfg.Contexts["local"].Grafana
			grafana.AuthMethod = method
			grafana.APIToken = "stored-token"
			grafana.OAuthToken = "stored-oauth"
			grafana.ProxyEndpoint = server.URL
			require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), cfg))
			_, err = execute(password)
			require.NoError(t, err)
			loaded, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
			require.NoError(t, err)
			assert.Equal(t, "basic", loaded.Contexts["local"].Grafana.AuthMethod)
			assert.Empty(t, loaded.Contexts["local"].Grafana.APIToken)
			assert.Empty(t, loaded.Contexts["local"].Grafana.OAuthToken)
		})
	}
}

func TestBasicLoginInvalidInputs(t *testing.T) {
	t.Setenv("GRAFANA_USER", "")
	t.Setenv("GRAFANA_PASSWORD", "")
	t.Setenv("GRAFANA_TOKEN", "")
	t.Setenv("GCX_KEYCHAIN", "off")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"token conflict", []string{"--basic-auth", "--token", "token"}, "mutually exclusive"},
		{"oauth conflict", []string{"--basic-auth", "--oauth"}, "mutually exclusive"},
		{"manual oauth conflict", []string{"--basic-auth", "--oauth-manual"}, "mutually exclusive"},
		{"user without method", []string{"--user", "admin"}, "--user requires --basic-auth"},
		{"explicit empty user", []string{"--basic-auth", "--user", ""}, "--user must not be empty"},
		{"whitespace user", []string{"--basic-auth", "--user", " "}, "--user must not be empty"},
		{"missing credentials", []string{"--basic-auth"}, "Login requires additional input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.NotFound(w, r)
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config.yaml")
			cmd := Command()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(append([]string{"--config", path, "--server", server.URL, "--yes"}, tt.args...))
			err := cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, tt.want)
			if strings.Contains(tt.want, "requires additional") {
				assert.Contains(t, fmt.Sprint(err), "GRAFANA_PASSWORD")
			}
			assert.Zero(t, requests.Load())
			_, err = os.Stat(path)
			assert.True(t, os.IsNotExist(err))
		})
	}
}

func TestBasicLoginRejectsRuntimeOnlyDestination(t *testing.T) {
	t.Setenv("GRAFANA_PASSWORD", "fresh-password")
	t.Setenv("GRAFANA_PROXY_ENDPOINT", "https://runtime.example.invalid")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	cmd := Command()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "config.yaml"), "--server", server.URL, "--basic-auth", "--user", "admin", "--yes"})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "runtime-only Grafana proxy/TLS settings")
	assert.Zero(t, requests.Load())
}

func TestReadBasicAuthEnvironment(t *testing.T) {
	t.Setenv("GRAFANA_USER", " env-user ")
	t.Setenv("GRAFANA_PASSWORD", " exported password ")
	for _, tt := range []struct {
		name         string
		interactive  bool
		user         string
		wantUser     string
		wantPassword string
	}{
		{"interactive with flag", true, "flag-user", "flag-user", ""},
		{"interactive with environment username", true, "", "env-user", ""},
		{"non-interactive with flag", false, "flag-user", "flag-user", " exported password "},
		{"non-interactive with environment username", false, "", "env-user", " exported password "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := &login.Options{Inputs: login.Inputs{GrafanaUser: tt.user}}
			readBasicAuthEnvironment(opts, tt.interactive)
			assert.Equal(t, tt.wantUser, opts.GrafanaUser)
			assert.Equal(t, tt.wantPassword, opts.GrafanaPassword)
		})
	}
}
