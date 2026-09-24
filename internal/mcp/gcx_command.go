package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func registerGCXCommandTool(s *sdkmcp.Server, buildCommand func() *cobra.Command) {
	s.AddTool(&sdkmcp.Tool{
		Name: "gcx_command",
		Description: "Execute any gcx CLI command and return its output. " +
			"gcx is a unified CLI for managing Grafana resources including dashboards, " +
			"alerting, synthetic monitoring, incidents, on-call, SLOs, and more.\n\n" +
			"Pass CLI arguments as a list of strings, exactly as you would type them " +
			"on the command line. Output is returned as JSON.\n\n" +
			"Examples:\n" +
			"  [\"synthetic-monitoring\", \"checks\", \"list\"]\n" +
			"  [\"synthetic-monitoring\", \"checks\", \"get\", \"my-check\"]\n" +
			"  [\"alerting\", \"rules\", \"list\"]\n" +
			"  [\"incidents\", \"list\", \"--status\", \"active\"]\n" +
			"  [\"dashboards\", \"list\"]\n" +
			"  [\"help-tree\"] — show all available commands\n\n" +
			"Use [\"help-tree\"] to discover available commands and their flags.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"args": {
					"type": "array",
					"items": {"type": "string"},
					"description": "CLI arguments as a list of strings"
				}
			},
			"required": ["args"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var input struct {
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return errorResult("invalid arguments: " + err.Error()), nil //nolint:nilerr // MCP tools return errors as tool results, not Go errors
		}
		if len(input.Args) == 0 {
			return errorResult("args must be a non-empty array of strings"), nil
		}

		return executeCommand(ctx, buildCommand, input.Args)
	})
}

func executeCommand(ctx context.Context, buildCommand func() *cobra.Command, args []string) (*sdkmcp.CallToolResult, error) {
	agent.SetFlag(true)

	hasOutputFlag := false
	for _, a := range args {
		if strings.HasPrefix(a, "-o") || strings.HasPrefix(a, "--output") {
			hasOutputFlag = true
			break
		}
	}
	if !hasOutputFlag {
		args = append(args, "--output", "json")
	}

	cmd := buildCommand()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(ctx)

	output := stdout.String()
	errOutput := stderr.String()

	if err != nil {
		msg := err.Error()
		if errOutput != "" {
			msg = errOutput
		}
		if output != "" {
			msg = output + "\n" + msg
		}
		return errorResult("command failed: " + msg), nil
	}

	if output == "" && errOutput != "" {
		return textResult(errOutput), nil
	}
	if output == "" {
		return textResult("(no output)"), nil
	}

	return textResult(output), nil
}

func textResult(text string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}

func errorResult(text string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}
