package mcpservers //nolint:testpackage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCreateOptsBuildInputMergesHeaders(t *testing.T) {
	opts := &createOpts{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Headers: []string{"Authorization=Bearer token"},
	}

	input, err := opts.buildInput()
	require.NoError(t, err)
	assert.Equal(t, "Remote MCP", input.Name)
	assert.Equal(t, "https://mcp.example.com/mcp", input.URL)
	require.Len(t, input.Headers, 1)
	assert.Equal(t, "Authorization", input.Headers[0].Name)
	assert.Equal(t, "Bearer token", input.Headers[0].Value)
}

func TestCreateOptsValidateRejectsInvalidScope(t *testing.T) {
	opts := &createOpts{
		Name:  "Remote MCP",
		URL:   "https://mcp.example.com/mcp",
		Scope: "stack",
	}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope must be one of: user, tenant")
}

func TestCreateOptsValidateRequiresHeadersForTenantScope(t *testing.T) {
	opts := &createOpts{
		Name:  "Remote MCP",
		URL:   "https://mcp.example.com/mcp",
		Scope: "tenant",
	}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateRequiresAuthHeaderForTenantScope(t *testing.T) {
	opts := &createOpts{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"X-Trace-ID=abc"},
	}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateRequiresAuthHeaderValueForTenantScope(t *testing.T) {
	opts := &createOpts{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"Authorization="},
	}

	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestCreateOptsValidateAcceptsAuthHeaderForTenantScope(t *testing.T) {
	opts := &createOpts{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []string{"Authorization=Bearer token"},
	}

	require.NoError(t, opts.Validate())
}
