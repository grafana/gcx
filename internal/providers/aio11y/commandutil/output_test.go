package commandutil_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/aio11y/commandutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldDefaultDetailToYAML(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		args      []string
		want      bool
	}{
		{
			name:      "defaults to yaml outside agent mode",
			agentMode: false,
			want:      true,
		},
		{
			name:      "keeps json default in agent mode",
			agentMode: true,
			want:      false,
		},
		{
			name:      "explicit output disables yaml override",
			agentMode: false,
			args:      []string{"--output=yaml"},
			want:      false,
		},
		{
			name:      "explicit json field selection disables yaml override",
			agentMode: false,
			args:      []string{"--json=field"},
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			cmd := &cobra.Command{Use: "show"}
			flags := cmd.Flags()
			flags.StringP("output", "o", "table", "")
			flags.String("json", "", "")
			require.NoError(t, flags.Parse(tc.args))

			assert.Equal(t, tc.want, commandutil.ShouldDefaultDetailToYAML(cmd))
		})
	}
}

func TestValidateDetailOutputFormat(t *testing.T) {
	cmd := &cobra.Command{Use: "show"}
	cmd.SetArgs([]string{"item"})

	t.Run("allows json output", func(t *testing.T) {
		err := commandutil.ValidateDetailOutputFormat(cmd, "json", "agent", "item")
		require.NoError(t, err)
	})

	t.Run("allows yaml output", func(t *testing.T) {
		err := commandutil.ValidateDetailOutputFormat(cmd, "yaml", "agent", "item")
		require.NoError(t, err)
	})

	for _, format := range []string{"table", "wide"} {
		t.Run("rejects_"+format, func(t *testing.T) {
			err := commandutil.ValidateDetailOutputFormat(cmd, format, "agent", "item")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not support -o "+format+" for a single agent")
			assert.Contains(t, err.Error(), "show item -o json")
		})
	}
}
