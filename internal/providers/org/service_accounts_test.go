package org //nolint:testpackage // Tests exercise private command constructors.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestServiceAccountCommands(t *testing.T) {
	tests := []struct {
		name               string
		makeCmd            func(*testLoader) *cobra.Command
		args               []string
		method, path, body string
		response           string
		wantError          string
	}{
		{name: "create", makeCmd: func(l *testLoader) *cobra.Command { return newAccountCreateCommand(l) }, args: []string{"robot", "--role=None"}, method: "POST", path: "/api/serviceaccounts", body: `{"name":"robot","role":"None","isDisabled":false}`, response: `{"id":12,"name":"robot","role":"None"}`},
		{name: "delete", makeCmd: func(l *testLoader) *cobra.Command { return newAccountDeleteCommand(l) }, args: []string{"12", "--force"}, method: "DELETE", path: "/api/serviceaccounts/12", response: `{}`},
		{name: "revoke", makeCmd: func(l *testLoader) *cobra.Command { return newTokenDeleteCommand(l) }, args: []string{"12", "4", "--force"}, method: "DELETE", path: "/api/serviceaccounts/12/tokens/4", response: `{}`},
		{name: "invalid role", makeCmd: func(l *testLoader) *cobra.Command { return newAccountCreateCommand(l) }, args: []string{"robot", "--role="}, wantError: "invalid role"},
		{name: "empty name", makeCmd: func(l *testLoader) *cobra.Command { return newAccountCreateCommand(l) }, args: []string{" "}, wantError: "name must not be empty"},
		{name: "invalid ID", makeCmd: func(l *testLoader) *cobra.Command { return newAccountDeleteCommand(l) }, args: []string{"0", "--force"}, wantError: "positive integer"},
		{name: "dry delete", makeCmd: func(l *testLoader) *cobra.Command { return newAccountDeleteCommand(l) }, args: []string{"12", "--dry-run"}},
		{name: "dry create", makeCmd: func(l *testLoader) *cobra.Command { return newAccountCreateCommand(l) }, args: []string{"robot", "--dry-run"}},
		{name: "decline", makeCmd: func(l *testLoader) *cobra.Command { return newAccountDeleteCommand(l) }, args: []string{"12"}, wantError: "use --force"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			t.Setenv("GCX_AUTO_APPROVE", "false")
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, tt.method, r.Method)
				assert.Equal(t, tt.path, r.URL.Path)
				if tt.body != "" {
					var actual, expected any
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&actual))
					assert.NoError(t, json.Unmarshal([]byte(tt.body), &expected))
					assert.Equal(t, expected, actual)
				}
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			loader := &testLoader{host: server.URL}
			cmd := tt.makeCmd(loader)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				require.Empty(t, out.String())
			} else {
				require.NoError(t, err)
				var result map[string]any
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				assert.Equal(t, "gcx.mutation", result["type"])
			}
			if tt.method == "" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestServiceAccountPagination(t *testing.T) {
	tests := []struct {
		name      string
		second    string
		wantError string
	}{
		{"complete", `{"totalCount":2,"serviceAccounts":[{"id":2,"name":"two"}]}`, ""},
		{"incomplete", `{"totalCount":2,"serviceAccounts":[]}`, "pagination stopped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/api/serviceaccounts/search", r.URL.Path)
				assert.Equal(t, "100", r.URL.Query().Get("perpage"))
				assert.Equal(t, []string{"1", "2"}[calls-1], r.URL.Query().Get("page"))
				if calls == 1 {
					_, _ = w.Write([]byte(`{"totalCount":2,"serviceAccounts":[{"id":1,"name":"one"}]}`))
				} else {
					_, _ = w.Write([]byte(tt.second))
				}
			}))
			defer server.Close()
			cmd := newAccountsListCommand(&testLoader{host: server.URL})
			var out, diag bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&diag)
			cmd.SetArgs([]string{"--limit=1"})
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			var result struct {
				Items []ServiceAccount `json:"items"`
				Meta  map[string]any   `json:"list_meta"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Items, 1)
			require.NotEmpty(t, result.Meta)
			require.Contains(t, diag.String(), "limit")
			require.Equal(t, 2, calls)
		})
	}
}

func TestTokenFile(t *testing.T) {
	tests := []struct {
		name                   string
		existing, dry          bool
		key                    string
		status                 int
		wantError              string
		wantCreate, wantRevoke int
		ttl                    string
		omitTTL                bool
		wantSeconds            int
	}{
		{name: "new file", key: "super-secret", status: 200, wantCreate: 1},
		{name: "day lifetime", ttl: "24h", wantSeconds: 86400, key: "super-secret", status: 200, wantCreate: 1},
		{name: "default lifetime", omitTTL: true, wantSeconds: 3600, key: "super-secret", status: 200, wantCreate: 1},
		{name: "no expiry", ttl: "0", key: "super-secret", status: 200, wantCreate: 1},
		{name: "existing file", existing: true, key: "super-secret", status: 200, wantError: "reserve token file"},
		{name: "dry run", dry: true, key: "super-secret", status: 200},
		{name: "missing key revokes", status: 200, wantError: "new token was revoked", wantCreate: 1, wantRevoke: 1},
		{name: "denied", status: 403, wantError: "permission denied", wantCreate: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			path := filepath.Join(t.TempDir(), "token.txt")
			if tt.existing {
				require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
			}
			creates, revokes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					revokes++
					assert.Equal(t, "/api/serviceaccounts/12/tokens/4", r.URL.Path)
					_, _ = w.Write([]byte(`{}`))
					return
				}
				creates++
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/api/serviceaccounts/12/tokens", r.URL.Path)
				var payload map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.Equal(t, "robot-token", payload["name"])
				wantSeconds := tt.wantSeconds
				if tt.ttl == "" && !tt.omitTTL {
					wantSeconds = 60
				}
				assert.InDelta(t, wantSeconds, payload["secondsToLive"], 0)
				w.WriteHeader(tt.status)
				if tt.status == 403 {
					_, _ = w.Write([]byte(`{"message":"permission denied"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 4, "name": "robot-token", "key": tt.key})
			}))
			defer server.Close()
			cmd := newTokenCreateCommand(&testLoader{host: server.URL})
			var out, diag bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&diag)
			args := []string{"12", "robot-token", "--output-file", path}
			if !tt.omitTTL {
				ttl := tt.ttl
				if ttl == "" {
					ttl = "60s"
				}
				args = append(args, "--ttl", ttl)
			}
			if tt.dry {
				args = append(args, "--dry-run")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCreate, creates)
			assert.Equal(t, tt.wantRevoke, revokes)
			assert.NotContains(t, out.String(), "super-secret")
			assert.NotContains(t, diag.String(), "super-secret")
			contents, readErr := os.ReadFile(path)
			switch {
			case tt.existing:
				require.NoError(t, readErr)
				assert.Equal(t, "original", string(contents))
			case tt.wantError == "" && !tt.dry:
				require.NoError(t, readErr)
				assert.Equal(t, tt.key+"\n", string(contents))
				if runtime.GOOS != "windows" {
					info, err := os.Stat(path)
					require.NoError(t, err)
					assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
				}
			default:
				require.True(t, os.IsNotExist(readErr))
			}
		})
	}
}

func TestRoles(t *testing.T) {
	tests := []struct {
		name        string
		assign, dry bool
		filter, uid string
		wantError   string
		wantPost    int
	}{
		{name: "filter", filter: "fixed:org.users"},
		{name: "case sensitive", filter: "FIXED"},
		{name: "assign", assign: true, uid: "writer-uid", wantPost: 1},
		{name: "dry", assign: true, dry: true, uid: "writer-uid"},
		{name: "custom role rejected", assign: true, uid: "custom-uid", wantError: "not a fixed role"},
		{name: "missing role", assign: true, uid: "missing", wantError: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					assert.Equal(t, "/api/access-control/users/12/roles", r.URL.Path)
					var body map[string]any
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, map[string]any{"roleUid": "writer-uid", "global": false}, body)
					_, _ = w.Write([]byte(`{}`))
					return
				}
				if strings.HasPrefix(r.URL.Path, "/api/serviceaccounts/") {
					assert.Equal(t, "/api/serviceaccounts/12", r.URL.Path)
					_, _ = w.Write([]byte(`{"id":12}`))
					return
				}
				assert.Equal(t, "/api/access-control/roles", r.URL.Path)
				_, _ = w.Write([]byte(`[{"uid":"writer-uid","name":"fixed:org.users:writer","global":true},{"uid":"custom-uid","name":"custom:role"}]`))
			}))
			defer server.Close()
			loader := &testLoader{host: server.URL}
			var cmd *cobra.Command
			if tt.assign {
				cmd = newAccountAddRoleCommand(loader)
				args := []string{"12", tt.uid}
				if tt.dry {
					args = append(args, "--dry-run")
				}
				cmd.SetArgs(args)
			} else {
				cmd = newRolesListCommand(loader)
				cmd.SetArgs([]string{"--filter", tt.filter})
			}
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantPost, posts)
			if !tt.assign {
				var result struct {
					Items []Role `json:"items"`
				}
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				if tt.filter == "FIXED" {
					require.Empty(t, result.Items)
					assert.Contains(t, out.String(), `"items":[]`)
				} else {
					require.Len(t, result.Items, 1)
					assert.Equal(t, "writer-uid", result.Items[0].UID)
				}
			}
		})
	}
}

func TestTokenValidationBeforeLoad(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"empty file", []string{"12", "name", "--output-file="}},
		{"empty name", []string{"12", " ", "--output-file=token.txt"}},
		{"fractional second", []string{"12", "name", "--output-file=token.txt", "--ttl=1500ms"}},
		{"empty lifetime", []string{"12", "name", "--output-file=token.txt", "--ttl="}},
		{"invalid lifetime", []string{"12", "name", "--output-file=token.txt", "--ttl=invalid"}},
		{"negative lifetime", []string{"12", "name", "--output-file=token.txt", "--ttl=-1s"}},
		{"invalid account", []string{"bad", "name", "--output-file=token.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			loader := &testLoader{}
			cmd := newTokenCreateCommand(loader)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			require.Error(t, cmd.Execute())
			require.Zero(t, loader.calls)
		})
	}
}

type accountAuthLoader struct{ host string }

func (l accountAuthLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.host, BearerToken: "caller-token", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}}, nil
}
func TestAccountAuth(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{{"authorized", 200}, {"unauthorized", 401}, {"forbidden", 403}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer caller-token", r.Header.Get("Authorization"))
				assert.Equal(t, "/api/serviceaccounts/search", r.URL.Path)
				w.WriteHeader(tt.status)
				if tt.status == 200 {
					_, _ = w.Write([]byte(`{"totalCount":0,"serviceAccounts":[]}`))
				} else {
					_, _ = w.Write([]byte(`{"message":"access denied"}`))
				}
			}))
			defer server.Close()
			cmd := newAccountsListCommand(accountAuthLoader{host: server.URL})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(nil)
			err := cmd.Execute()
			if tt.status == 200 {
				require.NoError(t, err)
				assert.Contains(t, out.String(), `"items":[]`)
			} else {
				require.Error(t, err)
				require.Empty(t, out.String())
			}
		})
	}
}

func TestOrgTableDefaults(t *testing.T) {
	tests := []struct {
		name    string
		makeCmd func(*testLoader) *cobra.Command
		args    []string
		want    []string
	}{
		{"accounts", func(l *testLoader) *cobra.Command { return newAccountsListCommand(l) }, nil, []string{"ID", "NAME", "robot"}},
		{"roles", func(l *testLoader) *cobra.Command { return newRolesListCommand(l) }, nil, []string{"UID", "NAME", "fixed:org.users:writer"}},
		{"create", func(l *testLoader) *cobra.Command { return newAccountCreateCommand(l) }, []string{"robot", "--dry-run"}, []string{"ACTION", "created", "robot", "true"}},
		{"delete", func(l *testLoader) *cobra.Command { return newAccountDeleteCommand(l) }, []string{"12", "--dry-run"}, []string{"ACTION", "deleted", "12", "true"}},
		{"assign", func(l *testLoader) *cobra.Command { return newAccountAddRoleCommand(l) }, []string{"12", "writer-uid", "--dry-run"}, []string{"ACTION", "assigned", "writer-uid", "true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/serviceaccounts/search":
					_, _ = w.Write([]byte(`{"totalCount":1,"serviceAccounts":[{"id":12,"name":"robot","role":"None"}]}`))
				case "/api/access-control/roles":
					_, _ = w.Write([]byte(`[{"uid":"writer-uid","name":"fixed:org.users:writer"}]`))
				case "/api/serviceaccounts/12":
					_, _ = w.Write([]byte(`{"id":12}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			cmd := tt.makeCmd(&testLoader{host: server.URL})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())
			for _, want := range tt.want {
				assert.Contains(t, out.String(), want)
			}
			assert.NotContains(t, out.String(), "schema_version:")
		})
	}
}

func TestTokenReceiptFormats(t *testing.T) {
	tests := []struct {
		name  string
		agent bool
		flags []string
		want  string
	}{
		{"human table", false, nil, "SUCCEEDED"},
		{"agent default", true, nil, `"type":"gcx.artifact_receipt"`},
		{"agent YAML", true, []string{"--output=yaml"}, "type: gcx.artifact_receipt"},
		{"agent jq", true, []string{"--jq=.files[0].path"}, `"token.txt"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, tt.agent)
			loader := &testLoader{}
			cmd := newTokenCreateCommand(loader)
			args := append([]string{"12", "name", "--output-file=token.txt", "--dry-run"}, tt.flags...)
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			require.NoError(t, cmd.Execute())
			require.Zero(t, loader.calls)
			assert.Contains(t, out.String(), tt.want)
		})
	}
}
