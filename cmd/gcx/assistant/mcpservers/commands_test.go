package mcpservers //nolint:testpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestCreateOptsValidateRequiresNameAndURL(t *testing.T) {
	opts := &createOpts{}
	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--name is required")

	opts.Name = "Remote MCP"
	err = opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--url is required")
}

func TestListOptsDefaultFormatAndAliases(t *testing.T) {
	opts := &listOpts{}
	flags := pflag.NewFlagSet("list", pflag.ContinueOnError)
	opts.setup(flags)

	assert.Equal(t, "text", opts.IO.OutputFormat)
	require.NoError(t, opts.IO.Validate())

	require.NoError(t, flags.Set("output", "table"))
	require.NoError(t, opts.IO.Validate())

	require.NoError(t, flags.Set("output", "wide"))
	require.NoError(t, opts.IO.Validate())
}

func TestListOptsValidateRejectsNegativePagination(t *testing.T) {
	opts := &listOpts{Limit: -1}
	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit must be non-negative")

	opts = &listOpts{Offset: -1}
	err = opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--offset must be non-negative")
}

func TestListAndCreateRejectPositionalArgs(t *testing.T) {
	require.Error(t, newListCommand(nil).Args(newListCommand(nil), []string{"extra"}))
	require.Error(t, newCreateCommand(nil).Args(newCreateCommand(nil), []string{"extra"}))
}

func TestCreateOptsBuildInputMergesHeaders(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Headers: []string{"Authorization=Bearer token"},
	}}

	input, err := opts.buildInput()
	require.NoError(t, err)
	assert.Equal(t, "Remote MCP", input.Name)
	assert.Equal(t, "https://mcp.example.com/mcp", input.URL)
	require.Len(t, input.Headers, 1)
	assert.Equal(t, "Authorization", input.Headers[0].Name)
	assert.Equal(t, "Bearer token", input.Headers[0].Value)
}

func TestCreateOptsValidateRejectsInvalidScope(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:  "Remote MCP",
		URL:   "https://mcp.example.com/mcp",
		Scope: "stack",
	}}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope must be one of: user, tenant")
}

func TestCreateOptsValidateRejectsInvalidURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "non-http scheme", raw: "ftp://mcp.example.com/mcp"},
		{name: "hostless", raw: "https:///mcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &createOpts{inputFlags: inputFlags{Name: "Remote MCP", URL: tt.raw}}

			err := opts.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--url")
		})
	}
}

func TestCreateOptsValidateRequiresHeadersForTenantScope(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:  "Remote MCP",
		URL:   "https://mcp.example.com/mcp",
		Scope: "tenant",
	}}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateRequiresAuthHeaderForTenantScope(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"X-Trace-ID=abc"},
	}}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateRequiresAuthHeaderValueForTenantScope(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"Authorization="},
	}}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateAcceptsAuthHeaderForTenantScope(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"Authorization=Bearer token"},
	}}

	require.NoError(t, opts.Validate())
}

func TestCreateOptsValidateRejectsTenantScopeWithEmailHeaderOnly(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"X-CH-Auth-Email=user@example.com"},
	}}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateAcceptsClickHouseTokenHeaderForTenantScope(t *testing.T) {
	opts := &createOpts{inputFlags: inputFlags{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"X-CH-Auth-Email=user@example.com", "X-CH-Auth-API-Token=token"},
	}}

	require.NoError(t, opts.Validate())
}

func TestDeletePromptsAndAbortsWithoutConfigLoad(t *testing.T) {
	cmd := newDeleteCommand(&providers.ConfigLoader{})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"GitHub"})

	require.NoError(t, cmd.Execute())
	// Prompts go to stderr so structured stdout stays machine-readable.
	assert.Contains(t, errOut.String(), `Delete MCP server "GitHub"?`)
	assert.Contains(t, errOut.String(), "Aborted.")
	assert.Empty(t, out.String())
}

func TestMaybeOpenAuthURLWarnsWhenBrowserOpenFails(t *testing.T) {
	origOpenURL := openURL
	openURL = func(string) error {
		return errors.New("browser unavailable")
	}
	t.Cleanup(func() { openURL = origOpenURL })

	cmd := newCreateCommand(nil)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	result := &assistantmcp.MutationResult{AuthURL: "https://example.com/oauth"}

	maybeOpenAuthURL(cmd, result)
	assert.Contains(t, stderr.String(), "Open the OAuth authorization URL manually")
	assert.Contains(t, stderr.String(), "https://example.com/oauth")
	assert.Contains(t, stderr.String(), "browser unavailable")
}

func newExistingResultTestClient(t *testing.T, integrations []map[string]any) *assistantmcp.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"integrations": integrations},
		}); err != nil {
			t.Errorf("failed to encode integrations response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return assistantmcp.NewClient(base)
}

func TestExistingResultMatchesNameURLAndScope(t *testing.T) {
	client := newExistingResultTestClient(t, []map[string]any{
		{"id": "mcp-tenant", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration": map[string]any{"url": "https://mcp.example.com/mcp"}},
	})
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	// Same name, different scope: not the requested server, create must proceed.
	_, found, err := existingResult(cmd, client, assistantmcp.ServerInput{
		Name: "GitHub", URL: "https://mcp.example.com/mcp", Scope: "user",
	})
	require.NoError(t, err)
	assert.False(t, found)

	// Same name, different URL: not the requested server either.
	_, found, err = existingResult(cmd, client, assistantmcp.ServerInput{
		Name: "GitHub", URL: "https://other.example.com/mcp", Scope: "tenant",
	})
	require.NoError(t, err)
	assert.False(t, found)

	result, found, err := existingResult(cmd, client, assistantmcp.ServerInput{
		Name: "GitHub", URL: "https://mcp.example.com/mcp", Scope: "tenant",
	})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "unchanged", result.Operation)
	assert.Equal(t, "mcp-tenant", result.Server.ID)
}
