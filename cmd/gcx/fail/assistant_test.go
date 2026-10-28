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
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssistantInvestigationsListForbidden(t *testing.T) {
	const basePath = "/api/plugins/grafana-assistant-app/resources"
	for _, tc := range []struct {
		name     string
		mode     string
		listPath string
		fallback bool
	}{
		{name: "access check", listPath: "/api/v2/investigations"},
		{name: "v2 list", mode: "v2", listPath: "/api/v2/investigations"},
		{name: "legacy list", mode: "v1", listPath: "/api/v1/investigations/summary"},
		{name: "legacy fallback", listPath: "/api/v1/investigations/summary", fallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			t.Setenv("GCX_ASSISTANT_API_VERSION", tc.mode)
			calls := 0
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
				_, _ = io.WriteString(w, `{"message":"insufficient permissions"}`)
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
			assert.Equal(t, []string{
				"Inspect your allowed actions: gcx api /api/access-control/user/actions",
				"Ask a Grafana admin to grant plugins.app:access for grafana-assistant-app",
				"Reading investigations also requires a role granting grafana-assistant-app.investigations:read",
			}, detailed.Suggestions)
			assert.Equal(t, docs.RolesAndPermissions, detailed.DocsLink)
			assert.Contains(t, detailed.Details, "insufficient permissions")
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
