// Package assistant registers the Grafana Assistant as a gcx provider.
package assistant

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
// so name alone is ambiguous (ADR Decision 3 / FR-011).
func (m MCPServer) GetResourceName() string {
	return m.Scope + "-" + adapter.SlugifyName(m.Name)
}

// SetResourceName is a no-op: scope and name are materialized directly in
// spec and are populated from the manifest's own fields during unmarshal.
// The composite metadata.name carries no information that isn't already in
// spec.scope/spec.name, and FR-011 forbids parsing scope back out of it.
func (m *MCPServer) SetResourceName(_ string) {}

// MCPServer is the manifest domain type for an assistant MCP server
// integration, distinct from the client's read type Server (redacts header
// values) and write type ServerInput (no preserve/remove/fromEnv concept).
// It materializes every user-editable field into spec so gcx resources
// get/pull/push/delete can round-trip it losslessly (FR-010).
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
	// addressing (FR-012). Unexported, so it is never serialized and never
	// participates in JSON round-trips, GetResourceName, or natural-key
	// matching — those read only Scope/Name/URL (FR-011, FR-013).
	serverID string
}

// MCPServerHeader models a single header's write intent on the manifest:
// a supplied Value means overwrite, a name-only header (no Value/FromEnv/
// FromFile) means preserve the stored secret on update, and a header
// omitted from the manifest entirely means remove. FromEnv/FromFile source
// the value from the environment or a file at push time; neither is ever
// persisted into a pulled manifest (FR-018 through FR-021, owned by T6).
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
