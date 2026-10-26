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
		name string
		args []string
		want bool
	}{
		{name: "no flags", want: false},
		{name: "-o text", args: []string{"-o", "text"}, want: true},
		{name: "-o wide", args: []string{"-o", "wide"}, want: true},
		{name: "-o yaml", args: []string{"-o", "yaml"}, want: true},
		{name: "-o json", args: []string{"-o", "json"}, want: false},
		{name: "-o agents", args: []string{"-o", "agents"}, want: false},
		{name: "-o pprof is an artifact format", args: []string{"-o", "pprof"}, want: false},
		{name: "--json fields", args: []string{"--json", "a,b"}, want: false},
		{name: "--jq", args: []string{"--jq", "."}, want: false},
		{name: "--jq with -o yaml keeps JSON errors", args: []string{"--jq", ".", "-o", "yaml"}, want: false},
		{name: "--json with -o yaml keeps JSON errors", args: []string{"--json", "a", "-o", "yaml"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
			cmd.Flags().StringP("output", "o", "agents", "")
			cmd.Flags().String("json", "", "")
			cmd.Flags().String("jq", "", "")
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.Execute())

			assert.Equal(t, tc.want, root.ExplicitHumanOutputForTest(cmd))
		})
	}

	t.Run("pinned default format keeps the stdout receipt", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
		var opts output.Options
		opts.PinDefaultFormat("json")
		opts.BindFlags(cmd.Flags())
		cmd.SetArgs([]string{"-o", "yaml"})
		require.NoError(t, cmd.Execute())
		assert.False(t, root.ExplicitHumanOutputForTest(cmd))
	})

	t.Run("command without an output flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
		require.NoError(t, cmd.Execute())
		assert.False(t, root.ExplicitHumanOutputForTest(cmd))
	})
}
