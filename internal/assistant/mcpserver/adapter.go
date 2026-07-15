package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// MCPServerIDAnnotation carries the server-assigned opaque ID for
// within-stack addressing. TypedCRUD strips metadata (including
// annotations) before handing a manifest's spec to CreateFn/UpdateFn/
// DeleteFn/GetFn, so every scope-qualified lookup in this file resolves
// purely from spec fields (scope, name, url) — never from this annotation
// and never by parsing metadata.name. It exists for display and
// within-stack round-trip only, and MUST NOT be used for cross-stack
// matching — that goes through the (scope, name, url) natural key
// registered in init() below.
const MCPServerIDAnnotation = MCPServerAPIGroup + "/server-id"

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		MCPServerDescriptor().GroupVersionKind(),
		adapter.SpecFieldKey("scope", "name", "url"),
	)
}

// NewTypedCRUD creates a TypedCRUD for MCPServer resources using the
// provided loader. List and every scope-qualified lookup (Get, Update,
// Delete) resolve through the client's exhausting ListAll rather than its
// single-page Get/Find, so large stacks are never truncated and a
// lookup is never restricted to the first page of the underlying
// integration list.
func NewTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[MCPServer], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load Grafana config for MCP servers: %w", err)
	}

	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create assistant HTTP client for MCP servers: %w", err)
	}
	client := assistantmcp.NewClient(base)

	return NewTypedCRUDForClient(client, cfg.Namespace), cfg, nil
}

// NewTypedCRUDForClient builds a TypedCRUD for MCPServer resources from an
// already-constructed client, bypassing config/loader resolution. Exported
// so tests can wire a client backed by an httptest.Server instead of a real
// Grafana config; NewTypedCRUD is the production entry point.
func NewTypedCRUDForClient(client *assistantmcp.Client, namespace string) *adapter.TypedCRUD[MCPServer] {
	crud := &adapter.TypedCRUD[MCPServer]{
		ListFn: adapter.LimitedListFn(func(ctx context.Context) ([]MCPServer, error) {
			servers, err := client.ListAll(ctx, assistantmcp.ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to list MCP servers: %w", err)
			}
			result := make([]MCPServer, 0, len(servers))
			for _, s := range servers {
				result = append(result, ServerToMCPServer(s))
			}
			return result, nil
		}),

		GetFn: func(ctx context.Context, name string) (*MCPServer, error) {
			server, err := findServerByResourceName(ctx, client, name)
			if err != nil {
				return nil, err
			}
			m := ServerToMCPServer(*server)
			return &m, nil
		},

		// CreateFn determines create-vs-update by the (scope, name, url)
		// natural key before applying header write intent: a natural-key
		// match means a server with this identity already exists elsewhere
		// in the pipeline's view (e.g. first-time cross-stack sync), so the
		// call is routed to update instead of creating a duplicate. Only
		// the true create path -- no natural-key match -- rejects a
		// name-only header, since there is no existing secret to preserve
		// there.
		CreateFn: func(ctx context.Context, item *MCPServer) (*MCPServer, error) {
			headers, err := ResolveHeaders(item.Headers)
			if err != nil {
				return nil, fmt.Errorf("failed to create MCP server %q: %w", item.Name, err)
			}

			current, err := findServerByKey(ctx, client, item.Scope, item.Name, item.URL)
			switch {
			case err == nil:
				return updateResolvedServer(ctx, client, current.ID, item, headers)
			case errors.Is(err, adapter.ErrNotFound):
				if guardErr := rejectNameOnlyHeaders(headers); guardErr != nil {
					return nil, fmt.Errorf("failed to create MCP server %q: %w", item.Name, guardErr)
				}
				return createResolvedServer(ctx, client, item, headers)
			default:
				return nil, fmt.Errorf("failed to resolve MCP server %q (scope %q) for create: %w", item.Name, item.Scope, err)
			}
		},

		UpdateFn: func(ctx context.Context, _ string, item *MCPServer) (*MCPServer, error) {
			current, err := findServerByKey(ctx, client, item.Scope, item.Name, item.URL)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve MCP server %q (scope %q) for update: %w", item.Name, item.Scope, err)
			}
			headers, err := ResolveHeaders(item.Headers)
			if err != nil {
				return nil, fmt.Errorf("failed to update MCP server %q: %w", item.Name, err)
			}
			return updateResolvedServer(ctx, client, current.ID, item, headers)
		},

		DeleteFn: func(ctx context.Context, name string) error {
			server, err := findServerByResourceName(ctx, client, name)
			if err != nil {
				return err
			}
			_, err = client.Delete(ctx, server.ID)
			if err != nil {
				return fmt.Errorf("failed to delete MCP server %q: %w", name, err)
			}
			return nil
		},

		Namespace: namespace,

		MetadataFn: func(m MCPServer) map[string]any {
			if m.serverID == "" {
				return nil
			}
			return map[string]any{
				"annotations": map[string]any{
					MCPServerIDAnnotation: m.serverID,
				},
			}
		},

		Descriptor: MCPServerDescriptor(),
	}

	return crud
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily
// from the default config file when invoked. Used by the provider's
// TypedRegistrations().
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// findServerByKey resolves the underlying server for a given natural key
// (scope, name, url) by exhausting the full integration list via ListAll —
// never the client's single-page Get/Find — so large stacks are never
// truncated. Scope is read from the caller's spec fields only, never
// parsed out of metadata.name.
func findServerByKey(ctx context.Context, client *assistantmcp.Client, scope, name, url string) (*assistantmcp.Server, error) {
	servers, err := client.ListAll(ctx, assistantmcp.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP servers: %w", err)
	}
	for i := range servers {
		s := &servers[i]
		if strings.EqualFold(s.Scope, scope) && strings.EqualFold(s.Name, name) && s.URL == url {
			return s, nil
		}
	}
	return nil, fmt.Errorf("MCP server %q (scope %q, url %q): %w", name, scope, url, adapter.ErrNotFound)
}

// findServerByResourceName resolves the underlying server whose computed
// composite name ({scope}-{slug(name)}, via GetResourceName) equals the
// given metadata.name. It never parses scope out of the name string — it
// computes each candidate's name forward from its own scope/name fields and
// compares.
//
// Two distinct servers sharing (scope, name) but differing by URL compute
// the same composite name. Rather than silently picking the first match,
// every candidate is collected and an ambiguous-match error listing them is
// returned when more than one is found — this lookup is
// used by both Get and Delete, so neither ever acts on the wrong server of
// an ambiguous pair.
func findServerByResourceName(ctx context.Context, client *assistantmcp.Client, name string) (*assistantmcp.Server, error) {
	servers, err := client.ListAll(ctx, assistantmcp.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP servers: %w", err)
	}
	var matches []*assistantmcp.Server
	for i := range servers {
		s := &servers[i]
		if (MCPServer{Name: s.Name, Scope: s.Scope}).GetResourceName() == name {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("MCP server %q: %w", name, adapter.ErrNotFound)
	case 1:
		return matches[0], nil
	default:
		return nil, ambiguousResourceNameError(name, matches)
	}
}

// ambiguousResourceNameError lists every candidate server sharing the same
// composite (scope, name)-derived metadata.name so the caller can disambiguate
// by URL or server ID instead of getting a silent, possibly
// wrong, match.
func ambiguousResourceNameError(name string, matches []*assistantmcp.Server) error {
	candidates := make([]string, 0, len(matches))
	for _, s := range matches {
		candidates = append(candidates, fmt.Sprintf("id=%s url=%s", s.ID, s.URL))
	}
	return fmt.Errorf(
		"MCP server %q is ambiguous: %d servers share the same scope and name but differ by URL (%s) — disambiguate by URL or server ID",
		name, len(matches), strings.Join(candidates, "; "),
	)
}

// rejectNameOnlyHeaders guards the true create path: there is no
// existing stored secret to preserve, so a resolved header with an empty
// value (name-only, no inline value/fromEnv/fromFile) is an actionable
// error rather than a silently valueless write.
func rejectNameOnlyHeaders(headers []assistantmcp.Header) error {
	for _, h := range headers {
		if h.Value == "" {
			return fmt.Errorf(
				"header %q has no value: creating a new MCP server requires a value for every header — set an inline value, or supply one via fromEnv or fromFile",
				h.Name,
			)
		}
	}
	return nil
}

// createResolvedServer sends item as a new server using the already-resolved
// header list. Shared by CreateFn's true-create branch.
func createResolvedServer(ctx context.Context, client *assistantmcp.Client, item *MCPServer, headers []assistantmcp.Header) (*MCPServer, error) {
	input := serverInputFromMCPServer(item, headers)
	result, err := client.Create(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP server %q: %w", item.Name, err)
	}
	if result.Server == nil {
		return nil, fmt.Errorf("assistant API did not return the created MCP server %q", item.Name)
	}
	m := ServerToMCPServer(*result.Server)
	return &m, nil
}

// updateResolvedServer sends item as an update to the server identified by
// id, using the already-resolved header list. Shared by UpdateFn and by
// CreateFn's natural-key-match branch (routing a create call at an existing
// server onto the update path instead of creating a duplicate).
func updateResolvedServer(ctx context.Context, client *assistantmcp.Client, id string, item *MCPServer, headers []assistantmcp.Header) (*MCPServer, error) {
	input := serverInputFromMCPServer(item, headers)
	result, err := client.Update(ctx, id, input)
	if err != nil {
		return nil, fmt.Errorf("failed to update MCP server %q: %w", item.Name, err)
	}
	if result.Server == nil {
		return nil, fmt.Errorf("assistant API did not return the updated MCP server %q", item.Name)
	}
	m := ServerToMCPServer(*result.Server)
	return &m, nil
}

// ServerToMCPServer converts a client Server (redacted header values) into
// the manifest domain type. Header values are never populated here — the
// client's Server.CustomHeaders only ever carries names, and
// headersFromServer (headers.go) marks every one of them for preserve.
func ServerToMCPServer(s assistantmcp.Server) MCPServer {
	return MCPServer{
		Name:         s.Name,
		Scope:        s.Scope,
		URL:          s.URL,
		Enabled:      s.Enabled,
		Description:  s.Description,
		Applications: s.Applications,
		Config:       configWithoutDerivedKeys(s.Configuration),
		Headers:      headersFromServer(s.CustomHeaders),
		serverID:     s.ID,
	}
}

// configWithoutDerivedKeys strips the keys the client derives onto
// Server.Configuration from other Server fields (url, builtinId), so
// spec.config only carries the user-supplied configuration, not a
// duplicate of spec.url.
func configWithoutDerivedKeys(cfg map[string]any) map[string]any {
	if len(cfg) == 0 {
		return nil
	}
	out := make(map[string]any, len(cfg))
	maps.Copy(out, cfg)
	delete(out, "url")
	delete(out, "builtinId")
	if len(out) == 0 {
		return nil
	}
	return out
}

// serverInputFromMCPServer converts the manifest domain type into the
// client's write type, given the already-resolved header list (via
// ResolveHeaders in headers.go -- inline value, fromEnv, and fromFile
// sourcing are all collapsed into a plain name+value list before this
// reaches Client.Create/Update, so those calls never see an unresolved
// fromEnv/fromFile reference). Headers are resolved by the caller, once,
// before it decides the create-vs-update path and applies the create-path
// name-only guard, so this function does not resolve or re-validate them.
// A resolved empty Value naturally preserves an existing stored header on
// update -- the client's Update derives per-header write intent
// (overwrite/preserve/remove) from the desired list, so that
// classification stays centralized at the client boundary and the
// wire-encoding assumption is never duplicated here.
func serverInputFromMCPServer(m *MCPServer, headers []assistantmcp.Header) assistantmcp.ServerInput {
	enabled := m.Enabled
	return assistantmcp.ServerInput{
		Name:         m.Name,
		Description:  m.Description,
		URL:          m.URL,
		Enabled:      &enabled,
		Scope:        m.Scope,
		Headers:      headers,
		Applications: m.Applications,
		Config:       m.Config,
	}
}
