package mcp

import (
	"errors"

	internalmcp "github.com/grafana/gcx/internal/mcp"
	appversion "github.com/grafana/gcx/internal/version"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// CommandBuilder is set by root/command.go to provide a function that builds
// a fresh root cobra command for programmatic execution. This breaks the
// import cycle: root → cmd/gcx/mcp (this package), and this package uses the
// builder without importing root.
//
//nolint:gochecknoglobals // Set by root/command.go to break the import cycle.
var CommandBuilder func() *cobra.Command

// Command returns the top-level "mcp" command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol server",
		Long:  "Start an MCP server that exposes gcx capabilities as tools for AI agents.",
	}
	cmd.AddCommand(serveCommand())
	return cmd
}

func serveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start an MCP server on stdio",
		Long:  "Start an MCP server listening on stdin/stdout using the JSON-RPC protocol. This is intended for use with AI agent tools like Claude Code.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if CommandBuilder == nil {
				return errors.New("mcp: CommandBuilder not set — this is a bug")
			}
			s := internalmcp.NewServer(appversion.Get(), CommandBuilder)
			return s.Run(cmd.Context(), &sdkmcp.StdioTransport{})
		},
	}
}
