package mcpserver

import (
	"context"
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
// within-stack addressing (FR-012). TypedCRUD strips metadata (including
// annotations) before handing a manifest's spec to CreateFn/UpdateFn/
// DeleteFn/GetFn, so every scope-qualified lookup in this file resolves
// purely from spec fields (scope, name, url) — never from this annotation
// and never by parsing metadata.name (FR-011). It exists for display and
// within-stack round-trip only, and MUST NOT be used for cross-stack
// matching (FR-012) — that goes through the (scope, name, url) natural key
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
// single-page Get/Find, so large stacks are never truncated (FR-015) and a
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

		CreateFn: func(ctx context.Context, item *MCPServer) (*MCPServer, error) {
			input, err := serverInputFromMCPServer(item)
			if err != nil {
				return nil, fmt.Errorf("failed to create MCP server %q: %w", item.Name, err)
			}
			result, err := client.Create(ctx, input)
			if err != nil {
				return nil, fmt.Errorf("failed to create MCP server %q: %w", item.Name, err)
			}
			if result.Server == nil {
				return nil, fmt.Errorf("assistant API did not return the created MCP server %q", item.Name)
			}
			m := ServerToMCPServer(*result.Server)
			return &m, nil
		},

		UpdateFn: func(ctx context.Context, _ string, item *MCPServer) (*MCPServer, error) {
			current, err := findServerByKey(ctx, client, item.Scope, item.Name, item.URL)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve MCP server %q (scope %q) for update: %w", item.Name, item.Scope, err)
			}
			input, err := serverInputFromMCPServer(item)
			if err != nil {
				return nil, fmt.Errorf("failed to update MCP server %q: %w", item.Name, err)
			}
			result, err := client.Update(ctx, current.ID, input)
			if err != nil {
				return nil, fmt.Errorf("failed to update MCP server %q: %w", item.Name, err)
			}
			if result.Server == nil {
				return nil, fmt.Errorf("assistant API did not return the updated MCP server %q", item.Name)
			}
			m := ServerToMCPServer(*result.Server)
			return &m, nil
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
// truncated (FR-015). Scope is read from the caller's spec fields only,
// never parsed out of metadata.name (FR-011).
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
// compares (FR-011).
//
// Collision detection for two distinct servers that compute the same name
// (FR-014/AC-008) is intentionally not implemented here — a later task
// (create-path guard + collision-as-error) adds the ambiguous-match error
// on top of this lookup; today the first match wins.
func findServerByResourceName(ctx context.Context, client *assistantmcp.Client, name string) (*assistantmcp.Server, error) {
	servers, err := client.ListAll(ctx, assistantmcp.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP servers: %w", err)
	}
	for i := range servers {
		s := &servers[i]
		if (MCPServer{Name: s.Name, Scope: s.Scope}).GetResourceName() == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("MCP server %q: %w", name, adapter.ErrNotFound)
}

// ServerToMCPServer converts a client Server (redacted header values) into
// the manifest domain type. Header values are never populated here — the
// client's Server.CustomHeaders only ever carries names, and
// headersFromServer (headers.go) marks every one of them for preserve
// (FR-021).
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
// client's write type. Headers are resolved via ResolveHeaders (headers.go)
// -- inline value, fromEnv, and fromFile sourcing are all collapsed into a
// plain name+value list before this reaches Client.Create/Update, so those
// calls never see an unresolved fromEnv/fromFile reference. A resolved
// empty Value naturally preserves an existing stored header on update, via
// Client.Update's internal HeaderWritesForUpdate classification (FR-018);
// this function does not itself classify overwrite/preserve/remove -- that
// stays centralized at the client boundary (T2) so the wire-encoding
// assumption is never duplicated.
func serverInputFromMCPServer(m *MCPServer) (assistantmcp.ServerInput, error) {
	headers, err := ResolveHeaders(m.Headers)
	if err != nil {
		return assistantmcp.ServerInput{}, fmt.Errorf("failed to resolve headers: %w", err)
	}
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
	}, nil
}
