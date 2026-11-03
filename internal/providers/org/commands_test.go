package org //nolint:testpackage // Tests cover unexported command constructors and identity resolution.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type testLoader struct {
	host  string
	calls int
}

func (l *testLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.host}}, nil
}

func TestResolve(t *testing.T) {
	users := []User{{UserID: 1, Login: "alice", Email: "alice@example.com"}, {UserID: 2, Login: "1", Email: "bob@example.com"}, {UserID: 3, Login: "duplicate"}, {UserID: 4, Email: "duplicate"}}
	tests := []struct {
		name, subject string
		id            int64
		wantError     string
	}{
		{"login", "alice", 1, ""}, {"email", "bob@example.com", 2, ""}, {"ID", "3", 3, ""},
		{"numeric login collides", "1", 0, "ambiguous"}, {"duplicate fields", "duplicate", 0, "ambiguous"},
		{"case sensitive", "Alice", 0, "not found"}, {"outside organization", "99", 0, "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := resolve(users, tt.subject)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.id, user.UserID)
			}
		})
	}
}

func TestCommands(t *testing.T) {
	tests := []struct {
		name                       string
		command                    string
		args                       []string
		agent                      bool
		input                      string
		failID                     string
		wantPaths                  []string
		wantError                  string
		succeeded, failed, skipped int
	}{
		{name: "list limit", command: "list", args: []string{"--limit=1", "-o=json"}},
		{name: "get", command: "get", args: []string{"alice", "-o=json"}},
		{name: "dry run", command: "delete", args: []string{"alice", "--dry-run", "-o=json"}, skipped: 1},
		{name: "force and duplicate", command: "delete", args: []string{"alice", "1", "--force", "-o=json"}, wantPaths: []string{"/api/org/users/1"}, succeeded: 1},
		{name: "auto approve", command: "delete", args: []string{"alice", "-o=json"}, wantPaths: []string{"/api/org/users/1"}, succeeded: 1},
		{name: "decline", command: "delete", args: []string{"alice", "-o=json"}, input: "no\n", wantError: "removal cancelled"},
		{name: "closed input", command: "delete", args: []string{"alice", "-o=json"}, wantError: "confirmation unavailable"},
		{name: "interactive approve", command: "delete", args: []string{"alice", "-o=json"}, input: "yes\n", wantPaths: []string{"/api/org/users/1"}, succeeded: 1},
		{name: "agent auto approve", command: "delete", args: []string{"alice"}, agent: true, wantPaths: []string{"/api/org/users/1"}, succeeded: 1},
		{name: "agent dry run", command: "delete", args: []string{"alice", "--dry-run"}, agent: true, skipped: 1},
		{name: "agent requires force", command: "delete", args: []string{"alice"}, agent: true, wantError: "use --force"},
		{name: "agent default result", command: "delete", args: []string{"alice", "--force"}, agent: true, wantPaths: []string{"/api/org/users/1"}, succeeded: 1},
		{name: "partial failure", command: "delete", args: []string{"alice", "bob", "--force", "-o=json"}, wantPaths: []string{"/api/org/users/1", "/api/org/users/2"}, failID: "2", wantError: "removal failed", succeeded: 1, failed: 1},
		{name: "resolve all before writes", command: "delete", args: []string{"alice", "missing", "--force", "-o=json"}, wantError: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, tt.agent)
			t.Setenv("GCX_AUTO_APPROVE", "false")
			if strings.Contains(tt.name, "auto approve") {
				t.Setenv("GCX_AUTO_APPROVE", "true")
			}
			var mu sync.Mutex
			paths := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					assert.Equal(t, "/api/org/users", r.URL.Path)
					_, _ = w.Write([]byte(`[{"orgId":7,"userId":1,"login":"alice","email":"alice@example.com","role":"Admin"},{"orgId":7,"userId":2,"login":"bob","role":"Viewer"}]`))
					return
				}
				assert.Equal(t, http.MethodDelete, r.Method)
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				if strings.HasSuffix(r.URL.Path, "/"+tt.failID) && tt.failID != "" {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"message":"denied"}`))
					return
				}
				_, _ = w.Write([]byte(`{"message":"User removed from organization"}`))
			}))
			defer server.Close()
			loader := &testLoader{host: server.URL}
			var cmd *cobra.Command
			switch tt.command {
			case "list":
				cmd = newListCommand(loader)
			case "get":
				cmd = newGetCommand(loader)
			default:
				cmd = newDeleteCommand(loader)
			}
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(tt.input))
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			require.ElementsMatch(t, tt.wantPaths, paths)
			if tt.command == "list" {
				var result listResult
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
				require.Len(t, result.Users, 1)
				require.NotNil(t, result.ListMeta)
				require.Equal(t, 1, result.ListMeta.Returned)
				require.Equal(t, 2, *result.ListMeta.Total)
				require.Contains(t, stderr.String(), "limit")
				return
			}
			if tt.command == "get" {
				var user User
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &user))
				require.Equal(t, int64(1), user.UserID)
				return
			}
			if tt.wantError != "" && tt.failed == 0 {
				if tt.name == "decline" {
					var detailed *gcxerrors.DetailedError
					require.ErrorAs(t, err, &detailed)
					require.NotNil(t, detailed.ExitCode)
					require.Equal(t, gcxerrors.ExitCancelled, *detailed.ExitCode)
				}
				require.Empty(t, stdout.String())
				return
			}
			var result deleteResult
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.Equal(t, "gcx.mutation_batch", result.Type)
			require.Equal(t, "removed", result.Action)
			require.Equal(t, tt.succeeded, result.Summary.Succeeded)
			require.Equal(t, tt.failed, result.Summary.Failed)
			require.Equal(t, tt.skipped, result.Summary.Skipped)
			if tt.name == "dry run" {
				require.True(t, result.DryRun)
				require.Equal(t, "1", result.Targets[0].ID)
			}
			if tt.failed > 0 {
				var emitted *gcxerrors.EmittedError
				require.ErrorAs(t, err, &emitted)
				require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
				require.Equal(t, "2", result.Failures[0].Target.ID)
			}
		})
	}
}

func TestValidationBeforeLoad(t *testing.T) {
	tests := []struct {
		name        string
		makeCommand func(*testLoader) *cobra.Command
		args        []string
	}{
		{"empty get", func(l *testLoader) *cobra.Command { return newGetCommand(l) }, []string{""}},
		{"empty delete", func(l *testLoader) *cobra.Command { return newDeleteCommand(l) }, []string{" "}},
		{"bad format", func(l *testLoader) *cobra.Command { return newListCommand(l) }, []string{"-o=unknown"}},
		{"negative limit", func(l *testLoader) *cobra.Command { return newListCommand(l) }, []string{"--limit=-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			loader := &testLoader{}
			cmd := tt.makeCommand(loader)
			cmd.SetArgs(tt.args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
			require.Zero(t, loader.calls)
		})
	}
}

func TestListResponses(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		wantError  string
	}{
		{name: "empty", body: "[]", status: 200},
		{name: "null", body: "null", status: 200},
		{name: "permission denied", body: `{"message":"permission denied"}`, status: 403, wantError: "permission denied"},
		{name: "invalid response", body: "invalid", status: 200, wantError: "decode organization users"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/org/users", r.URL.Path)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			cmd := newListCommand(&testLoader{host: server.URL})
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"-o=json"})
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				require.Empty(t, stdout.String())
				return
			}
			require.NoError(t, err)
			var result listResult
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.NotNil(t, result.Users)
			require.Empty(t, result.Users)
			require.Nil(t, result.ListMeta)
		})
	}
}

func TestUserResponseFields(t *testing.T) {
	// This fixture uses synthetic values. It covers all known membership fields.
	const userJSON = `{"orgId":7,"userId":42,"email":"alice@example.com","name":"Alice Example","login":"alice","role":"Viewer","lastSeenAt":"2026-01-02T03:04:05Z","lastSeenAtAge":"1 day","isDisabled":false,"isExternallySynced":true,"authLabels":["OAuth"],"avatarUrl":"https://example.com/avatar.png","created":"2025-01-02T03:04:05Z","isProvisioned":true,"uid":"example-user-uid"}`
	tests := []struct {
		name, format string
		get          bool
	}{
		{name: "list JSON", format: "json"}, {name: "list YAML", format: "yaml"},
		{name: "get JSON", format: "json", get: true}, {name: "get YAML", format: "yaml", get: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/org/users", r.URL.Path)
				_, _ = w.Write([]byte("[" + userJSON + "]"))
			}))
			defer server.Close()
			loader := &testLoader{host: server.URL}
			var cmd *cobra.Command
			if tt.get {
				cmd = newGetCommand(loader)
				cmd.SetArgs([]string{"alice", "-o=" + tt.format})
			} else {
				cmd = newListCommand(loader)
				cmd.SetArgs([]string{"-o=" + tt.format})
			}
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			require.NoError(t, cmd.Execute())
			var expected, actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(userJSON), &expected))
			if tt.get {
				require.NoError(t, yaml.Unmarshal(stdout.Bytes(), &actual))
			} else {
				var result struct {
					Users []map[string]any `yaml:"users"`
				}
				require.NoError(t, yaml.Unmarshal(stdout.Bytes(), &result))
				require.Len(t, result.Users, 1)
				actual = result.Users[0]
			}
			require.Equal(t, expected, actual)
		})
	}
}

func TestReadTableFormats(t *testing.T) {
	tests := []struct {
		name      string
		get, wide bool
	}{
		{name: "list default"}, {name: "list wide", wide: true},
		{name: "get default", get: true}, {name: "get wide", get: true, wide: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetAgentMode(t, false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/org/users", r.URL.Path)
				_, _ = w.Write([]byte(`[{"userId":42,"login":"alice","email":"alice@example.com","role":"Viewer","name":"Alice Example"}]`))
			}))
			defer server.Close()
			loader := &testLoader{host: server.URL}
			var cmd *cobra.Command
			args := []string{}
			if tt.get {
				cmd = newGetCommand(loader)
				args = append(args, "alice")
			} else {
				cmd = newListCommand(loader)
			}
			if tt.wide {
				args = append(args, "-o=wide")
			}
			cmd.SetArgs(args)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			require.NoError(t, cmd.Execute())
			require.Contains(t, stdout.String(), "ID")
			require.Contains(t, stdout.String(), "LOGIN")
			require.Contains(t, stdout.String(), "alice@example.com")
			if tt.wide {
				require.Contains(t, stdout.String(), "NAME")
				require.Contains(t, stdout.String(), "Alice Example")
			} else {
				require.NotContains(t, stdout.String(), "Alice Example")
			}
		})
	}
}

func TestListMetadataFormats(t *testing.T) {
	formats := []struct {
		name     string
		args     []string
		agent    bool
		selected bool
	}{
		{name: "JSON", args: []string{"-o=json"}},
		{name: "YAML", args: []string{"-o=yaml"}},
		{name: "agent", agent: true},
		{name: "field selection", args: []string{"--json=login"}, selected: true},
	}
	for _, format := range formats {
		for _, limit := range []int{0, 1, 2} {
			t.Run(format.name+"/limit="+strconv.Itoa(limit), func(t *testing.T) {
				testutils.SetAgentMode(t, format.agent)
				testutils.PinArgv(t, "gcx", "org", "users", "list", "--limit", strconv.Itoa(limit))
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodGet, r.Method)
					assert.Equal(t, "/api/org/users", r.URL.Path)
					_, _ = w.Write([]byte(`[{"userId":1,"login":"alice"},{"userId":2,"login":"bob"}]`))
				}))
				defer server.Close()
				cmd := newListCommand(&testLoader{host: server.URL})
				cmd.SetArgs(append([]string{"--limit=" + strconv.Itoa(limit)}, format.args...))
				var stdout, stderr bytes.Buffer
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				require.NoError(t, cmd.Execute())
				var result struct {
					Users    []map[string]any `yaml:"users"`
					ListMeta *cmdio.ListMeta  `yaml:"list_meta"`
				}
				require.NoError(t, yaml.Unmarshal(stdout.Bytes(), &result))
				if limit == 1 {
					require.Len(t, result.Users, 1)
					require.NotNil(t, result.ListMeta)
					require.True(t, result.ListMeta.Truncated)
					require.Equal(t, 1, result.ListMeta.Returned)
					require.NotNil(t, result.ListMeta.Total)
					require.Equal(t, 2, *result.ListMeta.Total)
					require.Equal(t, "gcx org users list --limit 0", result.ListMeta.Continue)
				} else {
					require.Len(t, result.Users, 2)
					require.Nil(t, result.ListMeta)
				}
				require.Equal(t, "alice", result.Users[0]["login"])
				if format.selected {
					require.Len(t, result.Users[0], 1)
				}
			})
		}
	}
}
