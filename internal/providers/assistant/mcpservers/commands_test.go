package mcpservers //nolint:testpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/mcpserver"
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

// TestRunListEmitsShowingFirstHintWhenMoreMayExist: the
// human list path stays bounded and prints a STDERR hint reading "showing
// first N -- use --limit for more" when more integrations may exist beyond
// the page -- never presenting the integration total as an MCP-server count.
func TestRunListEmitsShowingFirstHintWhenMoreMayExist(t *testing.T) {
	client := newExistingResultTestClient(t, []map[string]any{
		{"id": "mcp-1", "name": "Remote MCP", "type": "mcp", "enabled": true},
	})
	// The fake server ignores query params and always returns one MCP
	// integration with total:0 unset, so drive HasMore via a tiny --limit
	// that the single returned raw item meets/exceeds.
	opts := &listOpts{Limit: 1}
	opts.setup(pflag.NewFlagSet("list", pflag.ContinueOnError))
	opts.Limit = 1

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	require.NoError(t, runList(cmd, client, "default", opts))
	assert.Contains(t, stderr.String(), "showing first 1 — use --limit for more")
	assert.NotContains(t, stderr.String(), "of 1", "hint must never present the integration total as an MCP-server count")
}

func TestRunListNoHintWhenPageIsShort(t *testing.T) {
	client := newExistingResultTestClient(t, []map[string]any{
		{"id": "mcp-1", "name": "Remote MCP", "type": "mcp", "enabled": true},
	})
	opts := &listOpts{Limit: 50}
	opts.setup(pflag.NewFlagSet("list", pflag.ContinueOnError))
	opts.Limit = 50

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	require.NoError(t, runList(cmd, client, "default", opts))
	assert.Empty(t, stderr.String())
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

// TestFindByNaturalKeyMatchesScopeNameURL covers create's --if-not-exists
// pre-check: findByNaturalKey (through crud.List) matches only on the full
// (scope, name, url) natural key, so a same-name server differing by scope or
// URL is not treated as the requested server.
func TestFindByNaturalKeyMatchesScopeNameURL(t *testing.T) {
	client := newExistingResultTestClient(t, []map[string]any{
		{"id": "mcp-tenant", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration": map[string]any{"url": "https://mcp.example.com/mcp"}},
	})
	crud := mcpserver.NewTypedCRUDForClient(client, "default")

	// Same name, different scope: not the requested server, create must proceed.
	_, found, err := findByNaturalKey(t.Context(), crud, mcpserver.MCPServer{
		Name: "GitHub", URL: "https://mcp.example.com/mcp", Scope: "user",
	})
	require.NoError(t, err)
	assert.False(t, found)

	// Same name, different URL: not the requested server either.
	_, found, err = findByNaturalKey(t.Context(), crud, mcpserver.MCPServer{
		Name: "GitHub", URL: "https://other.example.com/mcp", Scope: "tenant",
	})
	require.NoError(t, err)
	assert.False(t, found)

	got, found, err := findByNaturalKey(t.Context(), crud, mcpserver.MCPServer{
		Name: "GitHub", URL: "https://mcp.example.com/mcp", Scope: "tenant",
	})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "mcp-tenant", got.Spec.ServerID())
}

// TestApplyUpdatePreservesHeadersWhenNoHeaderFlags covers the header-loss
// guard through the crud-routed update path: with no --header flags
// (input.Headers == nil) the desired manifest carries every current header as
// name-only, which the client boundary treats as preserve-existing.
func TestApplyUpdatePreservesHeadersWhenNoHeaderFlags(t *testing.T) {
	current := mcpserver.MCPServer{
		Name: "GitHub", Scope: "tenant", URL: "https://mcp.example.com/mcp", Enabled: true,
		Headers: []mcpserver.MCPServerHeader{{Name: "Authorization"}, {Name: "X-API-Key"}},
	}
	// No --header flags: ServerInput.Headers stays nil.
	disabled := false
	desired, err := applyUpdate(current, assistantmcp.ServerInput{Enabled: &disabled})
	require.NoError(t, err)
	assert.Equal(t, current.Headers, desired.Headers, "no --header flags must preserve every current header")
	assert.False(t, desired.Enabled, "non-identity field overlay must apply")
}

// TestApplyUpdateReplacesHeadersWhenHeaderFlagsGiven covers the full-desired
// header semantics: any --header flags become the complete header list, so an
// existing header not listed is dropped.
func TestApplyUpdateReplacesHeadersWhenHeaderFlagsGiven(t *testing.T) {
	current := mcpserver.MCPServer{
		Name: "GitHub", Scope: "tenant", URL: "https://mcp.example.com/mcp",
		Headers: []mcpserver.MCPServerHeader{{Name: "Authorization"}, {Name: "X-API-Key"}},
	}
	desired, err := applyUpdate(current, assistantmcp.ServerInput{
		Headers: []assistantmcp.Header{{Name: "X-API-Key", Value: "secret"}},
	})
	require.NoError(t, err)
	require.Len(t, desired.Headers, 1)
	assert.Equal(t, "X-API-Key", desired.Headers[0].Name)
	assert.Equal(t, "secret", desired.Headers[0].Value)
}

// TestApplyUpdateRejectsIdentityChange covers the immutable-identity rule:
// scope, name, and url form the natural key and cannot change via update.
func TestApplyUpdateRejectsIdentityChange(t *testing.T) {
	current := mcpserver.MCPServer{Name: "GitHub", Scope: "user", URL: "https://mcp.example.com/mcp"}
	tests := []struct {
		name  string
		input assistantmcp.ServerInput
		field string
	}{
		{"scope", assistantmcp.ServerInput{Scope: "tenant"}, "scope"},
		{"name", assistantmcp.ServerInput{Name: "GitLab"}, "name"},
		{"url", assistantmcp.ServerInput{URL: "https://other.example.com/mcp"}, "url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyUpdate(current, tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot change "+tt.field)
		})
	}
}

// TestApplyUpdateAcceptsMatchingIdentity covers that re-asserting the current
// identity (same scope/name/url) is not treated as a change.
func TestApplyUpdateAcceptsMatchingIdentity(t *testing.T) {
	current := mcpserver.MCPServer{Name: "GitHub", Scope: "user", URL: "https://mcp.example.com/mcp", Enabled: true}
	desired, err := applyUpdate(current, assistantmcp.ServerInput{
		Name: "github", Scope: "USER", URL: "https://mcp.example.com/mcp", Description: "updated",
	})
	require.NoError(t, err)
	assert.Equal(t, "updated", desired.Description)
	assert.Equal(t, "GitHub", desired.Name)
	assert.Equal(t, "user", desired.Scope)
}

// TestResolveServerRefByIDNameAndAmbiguity covers the crud.List-backed
// <id-or-name> resolution used by update/delete: exact ID match, display-name
// match, and an ambiguous name (two servers sharing scope+name) erroring
// instead of silently picking one.
func TestResolveServerRefByIDNameAndAmbiguity(t *testing.T) {
	client := newExistingResultTestClient(t, []map[string]any{
		{"id": "srv-1", "name": "GitHub", "type": "mcp", "scope": "tenant",
			"configuration": map[string]any{"url": "https://a.example.com/mcp"}},
		{"id": "srv-2", "name": "GitHub", "type": "mcp", "scope": "tenant",
			"configuration": map[string]any{"url": "https://b.example.com/mcp"}},
		{"id": "srv-3", "name": "Solo", "type": "mcp", "scope": "user",
			"configuration": map[string]any{"url": "https://c.example.com/mcp"}},
	})
	crud := mcpserver.NewTypedCRUDForClient(client, "default")

	got, err := resolveServerRef(t.Context(), crud, "srv-2")
	require.NoError(t, err)
	assert.Equal(t, "srv-2", got.Spec.ServerID())

	got, err = resolveServerRef(t.Context(), crud, "Solo")
	require.NoError(t, err)
	assert.Equal(t, "srv-3", got.Spec.ServerID())

	_, err = resolveServerRef(t.Context(), crud, "GitHub")
	var ambiguous assistantmcp.AmbiguousReferenceError
	require.ErrorAs(t, err, &ambiguous)

	_, err = resolveServerRef(t.Context(), crud, "missing")
	require.ErrorIs(t, err, assistantmcp.ErrNotFound)
}

// newRecordingTestClient serves a routed handler and records every request's
// "METHOD path" so a test can assert no mutating request was issued. The
// handler must serve the collection list, the singular GET by ID (used
// internally by the client's ref-resolving Delete), and DELETE by ID.
func newRecordingTestClient(t *testing.T, handler http.HandlerFunc) (*assistantmcp.Client, *[]string) {
	t.Helper()
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return assistantmcp.NewClient(base), &requests
}

// TestRunCreateErrorsOnExistingWithoutMutating is the regression test for
// credential loss reintroduced via create: a bare create (no
// --header flags, no --if-not-exists) against an existing (scope, name, url)
// match must FAIL rather than route into the adapter's upsert — which would
// resolve the empty desired-header list to "remove all" and strip the stored
// auth header. Evidence: the call errors and issues zero mutating requests, so
// the header-wipe path (a PUT) is unreachable.
func TestRunCreateErrorsOnExistingWithoutMutating(t *testing.T) {
	existing := map[string]any{
		"id": "srv-1", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
		"custom_headers": []map[string]any{{"name": "Authorization"}},
		"configuration":  map[string]any{"url": "https://mcp.example.com/mcp"},
	}
	client, requests := newRecordingTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"integrations": []map[string]any{existing}},
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	})
	crud := mcpserver.NewTypedCRUDForClient(client, "default")

	opts := &createOpts{}
	opts.setup(pflag.NewFlagSet("create", pflag.ContinueOnError))

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out bytes.Buffer
	cmd.SetOut(&out)

	manifest := mcpserver.MCPServer{Name: "GitHub", Scope: "tenant", URL: "https://mcp.example.com/mcp", Enabled: true}
	err := runCreate(cmd, crud, client, opts, manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Empty(t, out.String(), "no result may be encoded on the failure path")
	for _, req := range *requests {
		assert.Truef(t, strings.HasPrefix(req, http.MethodGet+" "),
			"create against an existing server must not issue a mutating request; saw %q", req)
	}
}

// TestRunCreateIfNotExistsReturnsUnchangedWithoutMutating confirms the fix does
// not regress the documented --if-not-exists no-op: an existing match returns
// "unchanged" and still issues no mutating request.
func TestRunCreateIfNotExistsReturnsUnchangedWithoutMutating(t *testing.T) {
	existing := map[string]any{
		"id": "srv-1", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
		"custom_headers": []map[string]any{{"name": "Authorization"}},
		"configuration":  map[string]any{"url": "https://mcp.example.com/mcp"},
	}
	client, requests := newRecordingTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"integrations": []map[string]any{existing}},
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	})
	crud := mcpserver.NewTypedCRUDForClient(client, "default")

	opts := &createOpts{}
	opts.setup(pflag.NewFlagSet("create", pflag.ContinueOnError))
	opts.IfNotExists = true

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out bytes.Buffer
	cmd.SetOut(&out)

	manifest := mcpserver.MCPServer{Name: "GitHub", Scope: "tenant", URL: "https://mcp.example.com/mcp", Enabled: true}
	require.NoError(t, runCreate(cmd, crud, client, opts, manifest))
	assert.Contains(t, out.String(), "unchanged")
	for _, req := range *requests {
		assert.Truef(t, strings.HasPrefix(req, http.MethodGet+" "),
			"--if-not-exists no-op must not issue a mutating request; saw %q", req)
	}
}

// TestRunDeleteByIDDeletesTargetedServerOnCollision is the regression test for
// the (scope, name)-collision delete bug: two servers share the composite
// metadata.name ({scope}-{slug(name)}) but differ by URL. delete <server-id>
// must resolve past the collision and delete only the targeted server —
// routing back through crud.Delete(current.Name) would re-hit the ambiguity on
// the composite name. Evidence: exactly the targeted ID is DELETEd.
func TestRunDeleteByIDDeletesTargetedServerOnCollision(t *testing.T) {
	servers := []map[string]any{
		{"id": "srv-1", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration": map[string]any{"url": "https://a.example.com/mcp"}},
		{"id": "srv-2", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
			"configuration": map[string]any{"url": "https://b.example.com/mcp"}},
	}
	var deletedIDs []string
	client, _ := newRecordingTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := path.Base(r.URL.Path) // "" collapses to "integrations" for the collection path
		switch {
		case r.Method == http.MethodDelete:
			deletedIDs = append(deletedIDs, id)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/integrations"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"integrations": servers}})
		default:
			for _, s := range servers {
				if s["id"] == id {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": s})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	})
	crud := mcpserver.NewTypedCRUDForClient(client, "default")

	opts := &deleteOpts{}
	opts.setup(pflag.NewFlagSet("delete", pflag.ContinueOnError))

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, runDelete(cmd, crud, client, opts, "srv-2"))
	assert.Equal(t, []string{"srv-2"}, deletedIDs, "only the targeted server ID may be deleted")
}
