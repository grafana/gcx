package mcpservers //nolint:testpackage

import (
	"bytes"
	"testing"

	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestListTableGolden(t *testing.T) {
	setAgentModeMCP(t, false)
	rows := []assistantmcp.Server{
		{ID: "server-1", Name: "Shared tools", Enabled: true, URL: "https://mcp.example.com/mcp", Scope: "tenant", Applications: []string{"assistant", "investigations"}},
		{ID: "server-2", Name: "Personal tools", URL: "https://personal.example.com/mcp", Scope: "user"},
	}
	for _, name := range []string{"text", "table", "wide"} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				rows []assistantmcp.Server
			}{
				{name: "rows", rows: rows},
				{name: "empty", rows: []assistantmcp.Server{}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					opts := &listOpts{}
					flags := pflag.NewFlagSet("list", pflag.ContinueOnError)
					opts.setup(flags)
					require.NoError(t, flags.Set("output", name))
					require.NoError(t, opts.Validate())
					var buf bytes.Buffer
					require.NoError(t, opts.IO.Encode(&buf, tc.rows))
					testutils.Golden(t, "list_"+name+"_"+tc.name, buf.String())
				})
			}
		})
	}
}
