package mcp

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// NewServer creates an MCP server with all gcx tools registered.
// buildCommand returns a fresh root cobra command for programmatic execution.
func NewServer(version string, buildCommand func() *cobra.Command) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "gcx",
		Version: version,
	}, &sdkmcp.ServerOptions{
		Instructions: "gcx is a unified CLI for managing Grafana resources. " +
			"This MCP server exposes gcx capabilities as tools. " +
			"Use gcx_command to run any gcx CLI command, or use the typed tools " +
			"for structured access to specific domains.",
	})

	registerGCXCommandTool(s, buildCommand)

	return s
}
