package mcpserver_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/assistant/mcpserver"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveHeaders_InlineValueOverwrites(t *testing.T) {
	resolved, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", Value: "Bearer secret"},
	})
	require.NoError(t, err)
	assert.Equal(t, []assistantmcp.Header{{Name: "Authorization", Value: "Bearer secret"}}, resolved)
}

func TestResolveHeaders_NameOnlyResolvesToEmptyValue(t *testing.T) {
	// Name-only is the preserve-on-update signal consumed by the client's
	// Update -- ResolveHeaders itself does not classify
	// overwrite/preserve/remove.
	resolved, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization"},
	})
	require.NoError(t, err)
	assert.Equal(t, []assistantmcp.Header{{Name: "Authorization", Value: ""}}, resolved)
}

func TestResolveHeaders_FromEnvResolvesValue(t *testing.T) {
	t.Setenv("GITHUB_MCP_TOKEN", "env-secret")

	resolved, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", FromEnv: "GITHUB_MCP_TOKEN"},
	})
	require.NoError(t, err)
	assert.Equal(t, []assistantmcp.Header{{Name: "Authorization", Value: "env-secret"}}, resolved)
}

func TestResolveHeaders_FromEnvUnsetErrors(t *testing.T) {
	_, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", FromEnv: "GCX_TEST_DOES_NOT_EXIST_MCP_TOKEN"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Authorization")
	assert.Contains(t, err.Error(), "GCX_TEST_DOES_NOT_EXIST_MCP_TOKEN")
}

func TestResolveHeaders_FromFileResolvesValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("file-secret\n"), 0o600))

	resolved, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", FromFile: path},
	})
	require.NoError(t, err)
	assert.Equal(t, []assistantmcp.Header{{Name: "Authorization", Value: "file-secret"}}, resolved)
}

func TestResolveHeaders_FromFileMissingErrors(t *testing.T) {
	_, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", FromFile: filepath.Join(t.TempDir(), "does-not-exist")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Authorization")
}

func TestResolveHeaders_FromFileEmptyErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	_, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", FromFile: path},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestResolveHeaders_MultipleSourcesErrors(t *testing.T) {
	_, err := mcpserver.ResolveHeaders([]mcpserver.MCPServerHeader{
		{Name: "Authorization", Value: "inline", FromEnv: "SOME_VAR"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one of value, fromEnv, or fromFile")
}

// TestServerToMCPServer_RedactsHeaderValues: converting a
// client Server into the manifest domain type must never populate a header
// Value/FromEnv/FromFile -- only Name survives, marking the header for
// preserve on a subsequent push.
func TestServerToMCPServer_RedactsHeaderValues(t *testing.T) {
	m := mcpserver.ServerToMCPServer(assistantmcp.Server{
		Name:  "GitHub",
		Scope: "tenant",
		CustomHeaders: []assistantmcp.ServerHeader{
			{Name: "Authorization", ValueConfigured: true},
		},
	})

	require.Len(t, m.Headers, 1)
	assert.Equal(t, "Authorization", m.Headers[0].Name)
	assert.Empty(t, m.Headers[0].Value)
	assert.Empty(t, m.Headers[0].FromEnv)
	assert.Empty(t, m.Headers[0].FromFile)
}
