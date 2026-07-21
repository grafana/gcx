package output_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHideFormat covers the advertised-menu suppression used by commands
// that reject a built-in display codec (resources pull/edit reject agents):
// the hidden name disappears from the usage string and the unknown-format
// error listing, while resolution stays intact so the command's own
// Validate owns the rejection.
func TestHideFormat(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	opts := &cmdio.Options{}
	opts.HideFormat("agents")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)

	// Usage string menu drops the hidden format.
	usage := flags.Lookup("output").Usage
	assert.NotContains(t, usage, "agents")
	assert.Contains(t, usage, "json")
	assert.Contains(t, usage, "yaml")

	// Unknown-format error listing drops it too.
	opts.OutputFormat = "bogus"
	err := opts.Validate()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "agents")
	assert.Contains(t, err.Error(), "json")

	// Resolution is unaffected: an explicit -o agents still validates here;
	// the command's own Validate is responsible for rejecting it.
	opts.OutputFormat = "agents"
	require.NoError(t, opts.Validate())
}

// TestBindFlags_PinDefaultFormat covers the file-writing-command opt-out:
// a pinned default survives the agent-mode "agents" override, while an
// explicit -o flag from the user still wins.
func TestBindFlags_PinDefaultFormat(t *testing.T) {
	tests := []struct {
		name           string
		agentMode      bool
		pin            string
		explicitOutput string // simulates -o flag; empty = use default
		wantFormat     string
	}{
		{
			name:       "agent mode keeps pinned default",
			agentMode:  true,
			pin:        "json",
			wantFormat: "json",
		},
		{
			name:           "explicit -o overrides pinned default in agent mode",
			agentMode:      true,
			pin:            "json",
			explicitOutput: "yaml",
			wantFormat:     "yaml",
		},
		{
			name:       "non-agent mode uses pinned default",
			agentMode:  false,
			pin:        "json",
			wantFormat: "json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			opts := &cmdio.Options{}
			opts.PinDefaultFormat(tc.pin)

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			if tc.explicitOutput != "" {
				require.NoError(t, flags.Set("output", tc.explicitOutput))
			}

			assert.Equal(t, tc.wantFormat, opts.OutputFormat)
		})
	}
}
