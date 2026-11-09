package fail_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	mcpcommands "github.com/grafana/gcx/internal/providers/assistant/mcpservers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestAssistantInvestigationsListForbidden(t *testing.T) {
	const basePath = "/api/plugins/grafana-assistant-app/resources"
	for _, tc := range []struct {
		name     string
		mode     string
		listPath string
		fallback bool
		proxy    bool
	}{
		{name: "access check", listPath: "/api/v2/investigations"},
		{name: "v2 list", mode: "v2", listPath: "/api/v2/investigations"},
		{name: "proxy route access check", listPath: "/api/v2/investigations", proxy: true},
		{name: "proxy route v2 list", mode: "v2", listPath: "/api/v2/investigations", proxy: true},
		{name: "proxy route legacy list", mode: "v1", listPath: "/api/v1/investigations/summary", proxy: true},
		{name: "legacy list", mode: "v1", listPath: "/api/v1/investigations/summary"},
		{name: "legacy fallback", listPath: "/api/v1/investigations/summary", fallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			t.Setenv("GCX_ASSISTANT_API_VERSION", tc.mode)
			calls := 0
			serverMessage := "insufficient permissions"
			if tc.proxy {
				serverMessage = "plugin proxy route access denied"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodGet, r.Method)
				if tc.fallback && calls == 1 {
					assert.Equal(t, basePath+"/api/v2/investigations", r.URL.Path)
					assert.Equal(t, "1", r.URL.Query().Get("limit"))
					w.WriteHeader(http.StatusNotFound)
					return
				}
				assert.Equal(t, basePath+tc.listPath, r.URL.Path)
				limit := "3"
				if tc.mode == "" && !tc.fallback {
					limit = "1"
				}
				assert.Equal(t, limit, r.URL.Query().Get("limit"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprintf(w, `{"message":%q}`, serverMessage)
			}))
			t.Cleanup(server.Close)
			cfgFile := filepath.Join(t.TempDir(), "config.yaml")
			cfg := fmt.Sprintf("version: 1\nstacks:\n  default:\n    grafana:\n      server: %s\n      stack-id: 12345\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n", server.URL)
			require.NoError(t, os.WriteFile(cfgFile, []byte(cfg), 0o600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfgFile)
			cmd := investigations.Commands(loader)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"list", "--limit", "3", "-o", "json"})
			err := cmd.Execute()
			require.Error(t, err)
			wantCalls := 1
			if tc.fallback {
				wantCalls++
			}
			assert.Equal(t, wantCalls, calls)
			assert.Empty(t, out.String())

			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, gcxerrors.SummaryAuthorizationFailed, detailed.Summary)
			require.NotNil(t, detailed.ExitCode)
			assert.Equal(t, 3, *detailed.ExitCode)
			suggestions := []string{"Inspect your allowed actions: gcx api /api/access-control/user/actions"}
			if tc.proxy {
				suggestions = append(suggestions, "Ask a Grafana admin to check the permission required by this Assistant plugin route")
				assert.NotContains(t, detailed.Details, "Ask a Grafana admin")
			} else {
				suggestions = append(suggestions, "Ask a Grafana admin to grant a role that includes grafana-assistant-app.investigations:read")
			}
			assert.Equal(t, suggestions, detailed.Suggestions)
			assert.Equal(t, docs.RolesAndPermissions, detailed.DocsLink)
			assert.Contains(t, detailed.Details, serverMessage)
			if tc.mode == "" && !tc.fallback {
				assert.Contains(t, detailed.Details, "check Grafana Assistant investigations access:")
			}
			assert.NotContains(t, detailed.Details, "probe")
			assert.NotContains(t, detailed.Details, "v2")

			require.NoError(t, detailed.WriteJSON(&out, *detailed.ExitCode))
			var result struct {
				Error struct {
					Summary     string   `json:"summary"`
					ExitCode    int      `json:"exitCode"`
					Suggestions []string `json:"suggestions"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			assert.Equal(t, detailed.Summary, result.Error.Summary)
			assert.Equal(t, 3, result.Error.ExitCode)
			assert.Subset(t, result.Error.Suggestions, detailed.Suggestions)
			assert.NotContains(t, out.String(), "gcx setup status")
			if tc.proxy {
				assert.NotContains(t, out.String(), "investigations:read")
			}
		})
	}
}

func TestAssistantInvestigationReadForbidden(t *testing.T) {
	const readAction = "grafana-assistant-app.investigations:read"
	const systemReadAction = "grafana-assistant-app.investigations.system:read"
	const systemReadBody = `{"name":"forbidden","message":"viewing system-created investigations requires the investigations.system:read permission","details":{"requiredPermission":"` + systemReadAction + `"}}`
	get := func(c *investigations.Client) error {
		_, err := c.Get(t.Context(), "inv-1")
		return err
	}
	for _, tc := range []struct {
		name, path, body, action string
		call                     func(*investigations.Client) error
		routeDenied              bool
	}{
		{name: "v1 get", path: "/api/v1/investigations/inv-1", body: systemReadBody, action: systemReadAction, call: get},
		{name: "v2 resolve", path: "/api/v2/investigations/inv-1", body: systemReadBody, action: systemReadAction, call: func(c *investigations.Client) error {
			_, _, err := c.ResolveByID(t.Context(), "inv-1")
			return err
		}},
		{name: "snapshot", path: "/api/v2/investigations/inv-1/snapshot", body: systemReadBody, action: systemReadAction, call: func(c *investigations.Client) error {
			_, err := c.GetState(t.Context(), "inv-1")
			return err
		}},
		{name: "evidence", path: "/api/v2/investigations/inv-1/evidence", body: systemReadBody, action: systemReadAction, call: func(c *investigations.Client) error {
			_, err := c.Evidence(t.Context(), "inv-1")
			return err
		}},
		{name: "absent action", path: "/api/v1/investigations/inv-1", body: `{"message":"insufficient permissions"}`, action: readAction, call: get},
		{name: "malformed JSON", path: "/api/v1/investigations/inv-1", body: `{"details":{"requiredPermission":"` + systemReadAction + `"}`, action: readAction, call: get},
		{name: "route denied precedence", path: "/api/v1/investigations/inv-1", body: `{"message":"plugin proxy route access denied","details":{"requiredPermission":"` + systemReadAction + `"}}`, call: get, routeDenied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugins/grafana-assistant-app/resources"+tc.path, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			err = tc.call(investigations.NewClient(base))
			require.Error(t, err)
			assert.Equal(t, 1, calls)
			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, gcxerrors.SummaryAuthorizationFailed, detailed.Summary)
			require.NotNil(t, detailed.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *detailed.ExitCode)
			suggestions := []string{"Inspect your allowed actions: gcx api /api/access-control/user/actions"}
			if tc.routeDenied {
				suggestions = append(suggestions, "Ask a Grafana admin to check the permission required by this Assistant plugin route")
				assert.NotContains(t, detailed.Details, "Ask a Grafana admin")
			} else {
				suggestions = append(suggestions, "Ask a Grafana admin to grant a role that includes "+tc.action)
			}
			assert.Equal(t, suggestions, detailed.Suggestions)
			assert.Contains(t, detailed.Details, tc.body)
			if tc.action == systemReadAction || tc.routeDenied {
				assert.NotContains(t, strings.Join(detailed.Suggestions, "\n"), readAction)
			}
		})
	}
}

func TestAssistantNonForbiddenAndOtherForbiddenUnchanged(t *testing.T) {
	for _, tc := range []struct {
		status      int
		other       bool
		summary     string
		exitCode    *int
		suggestions []string
	}{
		{
			status:      http.StatusUnauthorized,
			summary:     gcxerrors.SummaryAuthenticationFailed,
			exitCode:    new(gcxerrors.ExitAuthFailure),
			suggestions: []string{"Re-authenticate if needed: gcx login", "Verify the configured token is valid and has not expired"},
		},
		{status: http.StatusNotFound, summary: gcxerrors.SummaryResourceNotFound},
		{status: http.StatusInternalServerError, summary: gcxerrors.SummaryAPIError},
		{
			status:   http.StatusForbidden,
			other:    true,
			summary:  gcxerrors.SummaryAuthorizationFailed,
			exitCode: new(gcxerrors.ExitAuthFailure),
			suggestions: []string{
				"Check your roles and access-policy scopes",
				"Check your permissions: gcx setup status",
			},
		},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var err error
			if tc.other {
				err = &gcxerrors.HTTPStatusError{Status: tc.status, Message: "server explanation"}
			} else {
				err = assistanthttp.HandleErrorResponse(&http.Response{
					StatusCode: tc.status,
					Body:       io.NopCloser(strings.NewReader("server explanation")),
				})
			}
			var statusErr *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &statusErr)
			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, tc.summary, detailed.Summary)
			assert.Equal(t, tc.exitCode, detailed.ExitCode)
			assert.Equal(t, tc.suggestions, detailed.Suggestions)
			assert.Contains(t, detailed.Details, "server explanation")
			assert.Empty(t, detailed.DocsLink)
		})
	}
}

func TestAssistantMCPCreateForbidden(t *testing.T) {
	for _, tc := range []struct{ name, body, routeGuidance, action string }{
		{"backend", `{"message":"insufficient permissions"}`, "", ""},
		{"proxy route", `{"message":"plugin proxy route access denied"}`, "Ask a Grafana admin to check the permission required by this Assistant plugin route", ""},
		{"backend action", `{"details":{"requiredPermission":"grafana-assistant-app.mcp-servers:write"}}`, "", "grafana-assistant-app.mcp-servers:write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/api/plugins/grafana-assistant-app/resources/api/v1/integrations", r.URL.Path)
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"data":{"integrations":[],"total":0}}`)
					return
				}
				assert.Equal(t, http.MethodPost, r.Method)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			cfgFile := filepath.Join(t.TempDir(), "config.yaml")
			cfg := fmt.Sprintf("version: 1\nstacks:\n  default:\n    grafana:\n      server: %s\n      stack-id: 12345\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n", server.URL)
			require.NoError(t, os.WriteFile(cfgFile, []byte(cfg), 0o600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfgFile)
			cmd := mcpcommands.Commands(loader)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"create", "--name", "Tools", "--url", "https://mcp.example.com/mcp", "-o", "json"})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Equal(t, 3, calls)
			assert.Empty(t, out.String())
			detailed := fail.ErrorToDetailedError(err)
			assert.Equal(t, gcxerrors.SummaryAuthorizationFailed, detailed.Summary)
			require.NotNil(t, detailed.ExitCode)
			assert.Equal(t, gcxerrors.ExitAuthFailure, *detailed.ExitCode)
			assert.Contains(t, detailed.Details, tc.body)
			suggestions := []string{"Inspect your allowed actions: gcx api /api/access-control/user/actions"}
			if tc.action != "" {
				suggestions = append(suggestions, "Ask a Grafana admin to grant a role that includes "+tc.action)
			}
			if tc.routeGuidance != "" {
				suggestions = append(suggestions, tc.routeGuidance)
				assert.NotContains(t, detailed.Details, tc.routeGuidance)
			}
			assert.Equal(t, suggestions, detailed.Suggestions)
			assert.Equal(t, docs.RolesAndPermissions, detailed.DocsLink)
			require.NoError(t, detailed.WriteJSON(&out, *detailed.ExitCode))
			assert.NotContains(t, out.String(), "investigations:read")
			assert.NotContains(t, out.String(), "plugins.app:access")
			assert.NotContains(t, out.String(), "gcx setup status")
		})
	}
}
