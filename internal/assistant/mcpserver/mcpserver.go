// Package mcpserver holds the MCPServer manifest domain type and its
// TypedCRUD adapter wiring. It has no dependency on the assistant command
// tree, so it can be imported both by internal/providers/assistant (for
// adapter registration) and by internal/providers/assistant/mcpservers (for
// JSON/YAML output parity) without an import cycle.
package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// MCPServerAPIGroup is the API group for MCPServer resources.
	MCPServerAPIGroup = "assistant.ext.grafana.app"
	// MCPServerVersion is the API version for MCPServer resources.
	MCPServerVersion = "v1alpha1"
	// MCPServerAPIVersion is the full apiVersion string for MCPServer resources.
	MCPServerAPIVersion = MCPServerAPIGroup + "/" + MCPServerVersion
	// MCPServerKind is the kind for MCPServer resources.
	MCPServerKind = "MCPServer"
)

// GetResourceName returns the composite metadata.name for the manifest:
// {scope}-{slug(name)}. Server names are not unique and scope is required,
// so name alone is ambiguous (ADR-021 Decision 3).
func (m MCPServer) GetResourceName() string {
	return m.Scope + "-" + adapter.SlugifyName(m.Name)
}

// ServerID returns the server-assigned opaque ID carried on a manifest read
// back from the API (via ServerToMCPServer). It is empty for a manifest built
// from local input that has not yet been resolved against the server. Callers
// use it only for within-stack addressing (e.g. the OAuth validate/initiate
// step after create/update); it is never used for cross-stack matching.
func (m MCPServer) ServerID() string { return m.serverID }

// SetResourceName is a no-op: scope and name are materialized directly in
// spec and are populated from the manifest's own fields during unmarshal.
// The composite metadata.name carries no information that isn't already in
// spec.scope/spec.name, and scope is never parsed back out of it.
func (m *MCPServer) SetResourceName(_ string) {}

// MCPServer is the manifest domain type for an assistant MCP server
// integration, distinct from the client's read type Server (redacts header
// values) and write type ServerInput (no preserve/remove/fromEnv concept).
// It materializes every user-editable field into spec so gcx resources
// get/pull/push/delete can round-trip it losslessly.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type MCPServer struct {
	Name         string            `json:"name"`
	Scope        string            `json:"scope"`
	URL          string            `json:"url"`
	Enabled      bool              `json:"enabled"`
	Description  string            `json:"description,omitempty"`
	Applications []string          `json:"applications,omitempty"`
	Config       map[string]any    `json:"config,omitempty"`
	Headers      []MCPServerHeader `json:"headers,omitempty"`

	// serverID is the server-assigned opaque ID, carried only so the adapter's
	// MetadataFn can populate MCPServerIDAnnotation for within-stack
	// addressing. Unexported, so it is never serialized and never
	// participates in JSON round-trips, GetResourceName, or natural-key
	// matching — those read only Scope/Name/URL.
	serverID string
}

// MCPServerHeader models a single header's write intent on the manifest:
// a supplied Value means overwrite, a name-only header (no Value/FromEnv/
// FromFile) means preserve the stored secret on update, and a header
// omitted from the manifest entirely means remove. FromEnv/FromFile source
// the value from the environment or a file at push time; neither is ever
// persisted into a pulled manifest.
type MCPServerHeader struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	FromEnv  string `json:"fromEnv,omitempty"`
	FromFile string `json:"fromFile,omitempty"`
}

// MCPServerDescriptor returns the resource descriptor for MCPServer.
func MCPServerDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   MCPServerAPIGroup,
			Version: MCPServerVersion,
		},
		Kind:     MCPServerKind,
		Singular: "mcpserver",
		Plural:   "mcpservers",
	}
}

// MCPServerSchema returns a JSON Schema for the MCPServer resource type.
func MCPServerSchema() json.RawMessage {
	return adapter.SchemaFromType[MCPServer](MCPServerDescriptor())
}

// MCPServerExample returns an example MCPServer manifest as JSON.
func MCPServerExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": MCPServerAPIVersion,
		"kind":       MCPServerKind,
		"metadata": map[string]any{
			"name": "tenant-github",
		},
		"spec": map[string]any{
			"name":         "GitHub",
			"scope":        "tenant",
			"url":          "https://api.githubcopilot.com/mcp/",
			"enabled":      true,
			"description":  "GitHub MCP server for repository operations",
			"applications": []string{"assistant"},
			"config": map[string]any{
				"timeout": "30s",
			},
			"headers": []map[string]any{
				{"name": "Authorization", "fromEnv": "GITHUB_MCP_TOKEN"},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("assistant: failed to marshal MCPServer example: %v", err))
	}
	return b
}
