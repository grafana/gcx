package faro_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMutationErrors(t *testing.T) {
	const (
		appPath     = "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app"
		writeAction = "grafana-kowalski-app.apps:write"
		writeRole   = "Frontend Observability Editor"
		delAction   = "grafana-kowalski-app.apps:delete"
		delRole     = "Frontend Observability Admin"
	)
	app := &faro.FaroApp{Name: "my-app"}
	ops := []struct {
		name, method, path, wantMsg, action, role string
		call                                      func(ctx context.Context, c *faro.Client) error
	}{
		{"create", http.MethodPost, appPath, "faro: create app: status %d, body: %s", writeAction, writeRole,
			func(ctx context.Context, c *faro.Client) error { _, err := c.Create(ctx, app); return err }},
		{"update", http.MethodPut, appPath + "/42", "faro: update app 42: status %d, body: %s", writeAction, writeRole,
			func(ctx context.Context, c *faro.Client) error { _, err := c.Update(ctx, "42", app); return err }},
		{"delete", http.MethodDelete, appPath + "/42", "faro: delete app 42: status %d, body: %s", delAction, delRole,
			func(ctx context.Context, c *faro.Client) error { return c.Delete(ctx, "42") }},
		{"sourcemap delete", http.MethodDelete, appPath + "/42/sourcemaps/batch/b1", "faro: delete sourcemaps for app 42: status %d, body: %s", delAction, delRole,
			func(ctx context.Context, c *faro.Client) error { return c.DeleteSourcemaps(ctx, "42", []string{"b1"}) }},
	}
	responses := []struct {
		name   string
		status int
		body   string
		denied bool
	}{
		{"route denied", http.StatusForbidden, `{"message":"plugin proxy route access denied"}`, true},
		{"backend 403", http.StatusForbidden, `{"message":"forbidden"}`, false},
		{"not found", http.StatusNotFound, `{"error":"app not found"}`, false},
	}

	for _, op := range ops {
		for _, resp := range responses {
			t.Run(op.name+"/"+resp.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, op.method, r.Method)
					assert.Equal(t, op.path, r.URL.Path)
					w.WriteHeader(resp.status)
					_, _ = w.Write([]byte(resp.body))
				}))
				defer server.Close()

				err := op.call(t.Context(), newTestClient(t, server))
				require.Error(t, err)
				assert.Equal(t, fmt.Sprintf(op.wantMsg, resp.status, resp.body), err.Error())

				// Commands wrap client errors with their own context.
				detailed := fail.ErrorToDetailedError(fmt.Errorf("%s faro app %q: %w", op.name, "my-app", err))
				require.NotNil(t, detailed)
				if !resp.denied {
					assert.Nil(t, detailed.ExitCode, "other errors keep their previous exit code")
					assert.Contains(t, detailed.Error(), resp.body)
					return
				}

				wantSuggestions := []string{
					fmt.Sprintf("Ask a stack admin to grant you the %s role, which includes %s", op.role, op.action),
					"Check what your login holds: gcx setup status",
				}
				assert.Equal(t, "Authorization failed", detailed.Summary)
				assert.Equal(t, wantSuggestions, detailed.Suggestions)
				assert.NotEmpty(t, detailed.DocsLink)
				require.NotNil(t, detailed.ExitCode)
				assert.Equal(t, gcxerrors.ExitAuthFailure, *detailed.ExitCode)
				assert.Contains(t, detailed.Error(), `"my-app"`, "details keep the command's context")

				var buf bytes.Buffer
				require.NoError(t, detailed.WriteJSON(&buf, *detailed.ExitCode))
				var got struct {
					Error struct {
						Summary     string   `json:"summary"`
						Details     string   `json:"details"`
						ExitCode    int      `json:"exitCode"`
						Suggestions []string `json:"suggestions"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
				assert.Equal(t, "Authorization failed", got.Error.Summary)
				assert.Contains(t, got.Error.Details, `"my-app"`, "agent JSON keeps the command's context")
				assert.Contains(t, got.Error.Details, "missing "+op.action)
				assert.Equal(t, gcxerrors.ExitAuthFailure, got.Error.ExitCode)
				// Agent JSON appends a docs-fetch suggestion for DocsLink.
				assert.Subset(t, got.Error.Suggestions, wantSuggestions)
			})
		}
	}
}
