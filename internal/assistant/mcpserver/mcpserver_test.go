package mcpserver_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/grafana/gcx/internal/assistant/mcpserver"
	"github.com/grafana/gcx/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &mcpserver.MCPServer{}

func TestMCPServer_ResourceIdentity(t *testing.T) {
	tests := []struct {
		name string
		in   mcpserver.MCPServer
		want string
	}{
		{
			name: "tenant scope",
			in:   mcpserver.MCPServer{Name: "GitHub", Scope: "tenant"},
			want: "tenant-github",
		},
		{
			name: "user scope",
			in:   mcpserver.MCPServer{Name: "My Custom Server", Scope: "user"},
			want: "user-my-custom-server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.GetResourceName(); got != tt.want {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMCPServer_SetResourceNameIsNoOp(t *testing.T) {
	m := mcpserver.MCPServer{Name: "GitHub", Scope: "tenant"}
	m.SetResourceName("some-other-name")

	if m.Name != "GitHub" || m.Scope != "tenant" {
		t.Errorf("SetResourceName mutated fields: got Name=%q Scope=%q", m.Name, m.Scope)
	}
}

func TestMCPServer_JSONRoundTrip(t *testing.T) {
	original := mcpserver.MCPServer{
		Name:         "GitHub",
		Scope:        "tenant",
		URL:          "https://api.githubcopilot.com/mcp/",
		Enabled:      true,
		Description:  "GitHub MCP server",
		Applications: []string{"assistant", "copilot"},
		Config: map[string]any{
			"timeout": "30s",
		},
		Headers: []mcpserver.MCPServerHeader{
			{Name: "Authorization", Value: "secret-token"},
			{Name: "X-Preserve-Me"},
			{Name: "X-From-Env", FromEnv: "GITHUB_MCP_TOKEN"},
			{Name: "X-From-File", FromFile: "/etc/secrets/token"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var roundTripped mcpserver.MCPServer
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !reflect.DeepEqual(original, roundTripped) {
		t.Errorf("round trip mismatch:\n  original:     %#v\n  roundTripped: %#v", original, roundTripped)
	}
}

func TestMCPServer_SpecFieldsPresent(t *testing.T) {
	// A tenant-scoped server named GitHub, when marshaled, must
	// carry name, scope, url, enabled, and (where set) description,
	// applications, config, headers.
	server := mcpserver.MCPServer{
		Name:         "GitHub",
		Scope:        "tenant",
		URL:          "https://api.githubcopilot.com/mcp/",
		Enabled:      true,
		Description:  "GitHub MCP server",
		Applications: []string{"assistant"},
		Config:       map[string]any{"timeout": "30s"},
		Headers:      []mcpserver.MCPServerHeader{{Name: "Authorization"}},
	}

	data, err := json.Marshal(server)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	for _, field := range []string{"name", "scope", "url", "enabled", "description", "applications", "config", "headers"} {
		if _, ok := spec[field]; !ok {
			t.Errorf("spec missing field %q: %v", field, spec)
		}
	}

	if spec["name"] != "GitHub" {
		t.Errorf("spec[name] = %v, want %q", spec["name"], "GitHub")
	}
	if spec["scope"] != "tenant" {
		t.Errorf("spec[scope] = %v, want %q", spec["scope"], "tenant")
	}
}

func TestMCPServer_SpecFieldsOmitEmpty(t *testing.T) {
	// A minimal server (no description/applications/config/headers) must
	// still round-trip name/scope/url/enabled without the optional fields.
	server := mcpserver.MCPServer{
		Name:    "Minimal",
		Scope:   "user",
		URL:     "https://example.com/mcp/",
		Enabled: false,
	}

	data, err := json.Marshal(server)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	for _, field := range []string{"description", "applications", "config", "headers"} {
		if _, ok := spec[field]; ok {
			t.Errorf("spec should omit empty field %q, got: %v", field, spec[field])
		}
	}
}

func TestMCPServerSchema_NonNil(t *testing.T) {
	schema := mcpserver.MCPServerSchema()
	if schema == nil {
		t.Fatal("MCPServerSchema() = nil, want non-nil")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("MCPServerSchema() is not valid JSON: %v", err)
	}
}

func TestMCPServerExample_NonNil(t *testing.T) {
	example := mcpserver.MCPServerExample()
	if example == nil {
		t.Fatal("MCPServerExample() = nil, want non-nil")
	}

	var parsed map[string]any
	if err := json.Unmarshal(example, &parsed); err != nil {
		t.Fatalf("MCPServerExample() is not valid JSON: %v", err)
	}

	spec, ok := parsed["spec"].(map[string]any)
	if !ok {
		t.Fatalf("MCPServerExample() spec is not an object: %v", parsed)
	}
	for _, field := range []string{"name", "scope", "url", "enabled", "description", "applications", "config", "headers"} {
		if _, ok := spec[field]; !ok {
			t.Errorf("example spec missing field %q", field)
		}
	}
}

func TestMCPServerRegistration_SchemaAndExampleSetDirectly(t *testing.T) {
	// Schema and Example must be set on the Registration struct
	// directly, not relied upon via AsAdapter() (which doesn't propagate them).
	reg := adapter.Registration{
		Descriptor: mcpserver.MCPServerDescriptor(),
		GVK:        mcpserver.MCPServerDescriptor().GroupVersionKind(),
		Schema:     mcpserver.MCPServerSchema(),
		Example:    mcpserver.MCPServerExample(),
	}

	if reg.Schema == nil {
		t.Error("Registration.Schema is nil, want non-nil")
	}
	if reg.Example == nil {
		t.Error("Registration.Example is nil, want non-nil")
	}
	if reg.GVK.Kind != mcpserver.MCPServerKind {
		t.Errorf("Registration.GVK.Kind = %q, want %q", reg.GVK.Kind, mcpserver.MCPServerKind)
	}
}
