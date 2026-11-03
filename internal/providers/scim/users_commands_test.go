package scim //nolint:testpackage // Tests inject native bindings and local HTTP servers.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func userTestBinding(server string) native.Binding {
	c := &configClient{object: fixtureConfig()}
	return native.Fixed(native.Access{Client: c, Descriptor: configDescriptor(), Config: config.NamespacedRESTConfig{Config: rest.Config{Host: "https://untrusted.invalid/proxy", BearerToken: "context-token", Username: "context-user", Password: "context-password", WrapTransport: func(http.RoundTripper) http.RoundTripper { panic("context auth transport used") }}, GrafanaURL: server, Namespace: "stacks-12345"}})
}

func configDescriptor() resources.Descriptor {
	return resources.Descriptor{GroupVersion: schema.GroupVersion{Group: scimGroup, Version: "v0alpha1"}, Plural: "config", Kind: "SCIMConfig"}
}

func executeUsers(t *testing.T, binding native.Binding, input string, args ...string) (string, string, error) {
	t.Helper()
	cmd := newUsersCommand(binding)
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err := cmd.ExecuteContext(context.Background())
	return out.String(), stderr.String(), err
}

func tokenPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("service-token\r\n"), 0o600))
	return path
}

func TestUsersRequestContracts(t *testing.T) {
	tests := []struct {
		name                 string
		args                 []string
		method, suffix, body string
	}{
		{"get", []string{"get", "opaque-id"}, "GET", "/opaque-id", ""},
		{"create", []string{"create", "--user-name", "test@example.com", "--external-id", "ext-1", "--email", "test@example.com"}, "POST", "", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"test@example.com","externalId":"ext-1","active":true,"emails":[{"value":"test@example.com","type":"work","primary":true}]}`},
		{"update", []string{"update", "opaque-id", "--display-name", "Updated User"}, "PATCH", "/opaque-id", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"displayName":"Updated User"}}]}`},
		{"deactivate", []string{"deactivate", "opaque-id", "--force"}, "PATCH", "/opaque-id", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, tt.method, r.Method)
				assert.Equal(t, "/grafana/apis/scim.grafana.app/v0alpha1/namespaces/stacks-12345/Users"+tt.suffix, r.URL.Path)
				assert.Equal(t, "Bearer service-token", r.Header.Get("Authorization"))
				assert.Empty(t, r.Header.Get("Cookie"))
				assert.Equal(t, "application/scim+json", r.Header.Get("Accept"))
				if tt.body != "" {
					assert.Equal(t, "application/scim+json", r.Header.Get("Content-Type"))
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.JSONEq(t, tt.body, string(body))
				}
				_, _ = io.WriteString(w, `{"id":"opaque-id","userName":"test@example.com","extension":{"retained":true}}`)
			}))
			defer server.Close()
			args := append(append([]string{}, tt.args...), "--token-file", tokenPath(t), "-o", "json")
			out, stderr, err := executeUsers(t, userTestBinding(server.URL+"/grafana"), "", args...)
			require.NoError(t, err)
			require.Empty(t, stderr)
			require.Equal(t, 1, requests)
			if tt.name == "get" {
				require.JSONEq(t, `{"id":"opaque-id","userName":"test@example.com","extension":{"retained":true}}`, out)
			} else {
				var result struct {
					Type string       `json:"type"`
					User userResource `json:"user"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Equal(t, "gcx.mutation", result.Type)
				require.Equal(t, "opaque-id", result.User["id"])
				require.Equal(t, map[string]any{"retained": true}, result.User["extension"])
			}
			require.NotContains(t, out, "service-token")
		})
	}
}

func TestUsersCompletePagination(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, strconv.Itoa(calls), r.URL.Query().Get("startIndex"))
		assert.Equal(t, "100", r.URL.Query().Get("count"))
		assert.Equal(t, `userName eq "test@example.com"`, r.URL.Query().Get("filter"))
		_, _ = fmt.Fprintf(w, `{"totalResults":2,"startIndex":%d,"itemsPerPage":1,"Resources":[{"id":"%d"}]}`, calls, calls)
	}))
	defer server.Close()
	out, _, err := executeUsers(t, userTestBinding(server.URL), "service-token\n", "list", "--filter", `userName eq "test@example.com"`, "--token-file", "-", "-o", "json")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.JSONEq(t, `[{"id":"1"},{"id":"2"}]`, out)
}

func TestUsersRejectIncompletePagination(t *testing.T) {
	for _, response := range []string{
		`{"Resources":[]}`,
		`{"totalResults":2,"Resources":[]}`,
		`{"totalResults":2,"startIndex":1,"itemsPerPage":1,"Resources":[{"id":"1"}]}`,
		`{"totalResults":1,"itemsPerPage":2,"Resources":[{"id":"1"}]}`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, response) }))
			defer server.Close()
			out, _, err := executeUsers(t, userTestBinding(server.URL), "", "list", "--token-file", tokenPath(t), "-o", "json")
			require.Error(t, err)
			require.Empty(t, out)
		})
	}
}

func TestUsersEmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"totalResults":0,"Resources":[]}`)
	}))
	defer server.Close()
	out, _, err := executeUsers(t, userTestBinding(server.URL), "", "list", "--token-file", tokenPath(t), "-o", "json")
	require.NoError(t, err)
	require.JSONEq(t, `[]`, out)
}

func TestUsersRedirectAndErrors(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { targetCalls++ }))
	defer target.Close()
	for _, status := range []int{302, 307, 403, 404, 409} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"scimType":"invalidValue","detail":"denied service-token"}`)
			}))
			defer server.Close()
			out, stderr, err := executeUsers(t, userTestBinding(server.URL), "", "get", "1", "--token-file", tokenPath(t), "-o", "json")
			require.ErrorContains(t, err, fmt.Sprintf("HTTP %d", status))
			require.ErrorContains(t, err, "invalidValue")
			require.NotContains(t, err.Error(), "service-token")
			require.NotContains(t, err.Error(), "sync is disabled")
			require.Empty(t, out)
			require.Empty(t, stderr)
		})
	}
	require.Zero(t, targetCalls)
}

func TestUsersDryRunAndConfirmation(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { writes++ }))
	defer server.Close()
	for _, args := range [][]string{
		{"deactivate", "1"}, {"update", "1", "--display-name", "New Name"}, {"create", "--user-name", "test@example.com", "--external-id", "ext-1"},
	} {
		args = append(args, "--token-file", tokenPath(t), "--dry-run", "-o", "json")
		out, _, err := executeUsers(t, userTestBinding(server.URL), "", args...)
		require.NoError(t, err)
		var preview map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &preview))
		require.Equal(t, true, preview["dry_run"])
		require.Equal(t, map[string]string{"create": "created", "update": "updated", "deactivate": "deactivated"}[args[0]], preview["action"])
	}
	testutils.SetAgentMode(t, false)
	t.Setenv("GCX_AUTO_APPROVE", "false")
	_, _, err := executeUsers(t, userTestBinding(server.URL), "n\n", "deactivate", "1", "--token-file", tokenPath(t))
	require.ErrorContains(t, err, "cancelled")
	_, _, err = executeUsers(t, userTestBinding(server.URL), "service-token", "deactivate", "1", "--token-file", "-")
	require.ErrorContains(t, err, "use --force")
	require.Zero(t, writes)
}

func TestUsersValidationBeforeIO(t *testing.T) {
	for _, args := range [][]string{
		{"list"}, {"get"}, {"get", "1", "2"}, {"get", ".."}, {"get", "a/b"}, {"create"}, {"update", "1"}, {"update", "1", "--email="}, {"create", "--user-name", "test@example.com", "--external-id="}, {"list", "--filter="},
	} {
		if len(args) != 1 || args[0] != "list" {
			args = append(args, "--token-file", "/missing-token")
		}
		_, _, err := executeUsers(t, native.Binding{}, "", args...)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "open SCIM token file")
	}
}

func TestReadUserToken(t *testing.T) {
	token, err := readUserToken(strings.NewReader(" token \r\n"))
	require.NoError(t, err)
	require.Equal(t, " token ", token)
	for _, data := range []string{"", "\r\n", strings.Repeat("x", maxTokenBytes+1)} {
		_, err := readUserToken(strings.NewReader(data))
		require.Error(t, err)
	}
}

func TestUsersPatchNoContent(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `{"id":"1","active":false}`)
	}))
	defer server.Close()
	out, _, err := executeUsers(t, userTestBinding(server.URL), "", "deactivate", "1", "--force", "--token-file", tokenPath(t), "-o", "json")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	var result struct {
		User userResource `json:"user"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Equal(t, false, result.User["active"])
}

func TestUsersHumanAndAgentOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"totalResults":1,"Resources":[{"id":"1","userName":"test@example.com","active":true}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"1","userName":"test@example.com","active":true}`)
	}))
	defer server.Close()
	testutils.SetAgentMode(t, false)
	path := tokenPath(t)
	out, _, err := executeUsers(t, userTestBinding(server.URL), "", "list", "--token-file", path)
	require.NoError(t, err)
	require.Contains(t, out, "USER NAME")
	require.Contains(t, out, "test@example.com")
	out, _, err = executeUsers(t, userTestBinding(server.URL), "", "create", "--user-name", "test@example.com", "--external-id", "ext-1", "--token-file", path)
	require.NoError(t, err)
	require.Contains(t, out, `Created SCIM user "test@example.com"`)
	testutils.SetAgentMode(t, true)
	out, _, err = executeUsers(t, userTestBinding(server.URL), "", "create", "--user-name", "test@example.com", "--external-id", "ext-1", "--token-file", path)
	require.NoError(t, err)
	decoder := json.NewDecoder(strings.NewReader(out))
	var result map[string]any
	require.NoError(t, decoder.Decode(&result))
	require.Equal(t, "gcx.mutation", result["type"])
	require.ErrorIs(t, decoder.Decode(&result), io.EOF)
	_, _, err = executeUsers(t, userTestBinding(server.URL), "", "deactivate", "1", "--token-file", path)
	require.ErrorContains(t, err, "use --force")
}
