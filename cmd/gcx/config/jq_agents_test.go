package config_test

import (
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestListContexts_JQAgents(t *testing.T) {
	for _, agentMode := range []bool{false, true} {
		for _, jqFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("agent=%t/jqFirst=%t", agentMode, jqFirst), func(t *testing.T) {
				testutils.SetAgentMode(t, agentMode)
				t.Setenv("GCX_AGENT_SPILL_BYTES", "2") // Full contexts spill, the transformed count fits.
				t.Setenv("TMPDIR", t.TempDir())
				configFile := testutils.CreateTempFile(t, "version: 1\ncurrent-context: first\ncontexts:\n  first: {}\n  second: {}\n")
				args := []string{"list-contexts", "--config", configFile}
				if jqFirst {
					args = append(args, "--jq", ".contexts | length", "-o", "agents")
				} else {
					args = append(args, "-o", "agents", "--jq", ".contexts | length")
				}
				stdout, err := runConfigCommand(t, args...)
				require.NoError(t, err)
				require.Equal(t, "2\n", stdout)
			})
		}
	}
}
