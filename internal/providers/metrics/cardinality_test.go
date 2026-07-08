package metrics_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/metrics"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeCardinality(t *testing.T, args ...string) error {
	t.Helper()
	cmd := metrics.CardinalityCommands(nil)
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func TestCardinalityCommands_Structure(t *testing.T) {
	cmd := metrics.CardinalityCommands(nil)
	assert.Equal(t, "cardinality", cmd.Name())

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.ElementsMatch(t, []string{"label-names", "label-values"}, names)
}

// TestCardinalityValidation drives validation through the wired command tree so
// bad flags are rejected before any datasource resolution (nil loader is never
// reached).
func TestCardinalityValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "limit too high", args: []string{"label-names", "--limit=999"}, wantErr: "--limit"},
		{name: "limit negative", args: []string{"label-names", "--limit=-1"}, wantErr: "--limit"},
		{name: "bad count method", args: []string{"label-names", "--count-method=bogus"}, wantErr: "--count-method"},
		{name: "label-values missing labels", args: []string{"label-values"}, wantErr: "--label"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := executeCardinality(t, tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestCardinalityFlags(t *testing.T) {
	byName := map[string]*cobra.Command{}
	for _, sub := range metrics.CardinalityCommands(nil).Commands() {
		byName[sub.Name()] = sub
	}

	// --label (with -l shorthand) exists only on label-values.
	require.NotNil(t, byName["label-values"].Flags().Lookup("label"))
	require.NotNil(t, byName["label-values"].Flags().ShorthandLookup("l"))
	assert.Nil(t, byName["label-names"].Flags().Lookup("label"))
}
