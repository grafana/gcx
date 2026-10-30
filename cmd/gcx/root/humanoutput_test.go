package root_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitHumanOutput(t *testing.T) {
	tests := []struct {
		name  string
		setup func(opts *output.Options)
		args  []string
		want  bool
	}{
		{name: "no flags", want: false},
		{name: "-o text", args: []string{"-o", "text"}, want: true},
		{name: "-o wide", args: []string{"-o", "wide"}, want: true},
		{name: "-o yaml", args: []string{"-o", "yaml"}, want: true},
		{name: "-o json", args: []string{"-o", "json"}, want: false},
		{name: "-o agents", args: []string{"-o", "agents"}, want: false},
		{name: "--json fields", args: []string{"--json", "a,b"}, want: false},
		{name: "--jq", args: []string{"--jq", "."}, want: false},
		{name: "--jq with -o yaml keeps JSON errors", args: []string{"--jq", ".", "-o", "yaml"}, want: false},
		{name: "--json with -o yaml keeps JSON errors", args: []string{"--json", "a", "-o", "yaml"}, want: false},
		{
			name:  "pinned default format keeps the stdout receipt",
			setup: func(opts *output.Options) { opts.PinDefaultFormat("json") },
			args:  []string{"-o", "yaml"},
			want:  false,
		},
		{
			name:  "artifact format keeps the stdout receipt",
			setup: func(opts *output.Options) { opts.MarkArtifactFormat("pprof") },
			args:  []string{"-o", "pprof"},
			want:  false,
		},
		{
			name:  "other formats of a command with an artifact format stay human",
			setup: func(opts *output.Options) { opts.MarkArtifactFormat("pprof") },
			args:  []string{"-o", "yaml"},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
			var opts output.Options
			if tc.setup != nil {
				tc.setup(&opts)
			}
			opts.BindFlags(cmd.Flags())
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.Execute())

			assert.Equal(t, tc.want, root.ExplicitHumanOutputForTest(cmd))
		})
	}

	t.Run("a -o that is not an output-format flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
		cmd.Flags().StringP("output", "o", "", "Output directory")
		cmd.SetArgs([]string{"-o", "custom-rules"})
		require.NoError(t, cmd.Execute())
		assert.False(t, root.ExplicitHumanOutputForTest(cmd))
	})

	t.Run("command without an output flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
		require.NoError(t, cmd.Execute())
		assert.False(t, root.ExplicitHumanOutputForTest(cmd))
	})
}
