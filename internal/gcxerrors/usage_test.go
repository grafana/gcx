package gcxerrors_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandUsageError(t *testing.T) {
	root := &cobra.Command{Use: "gcx"}
	group := &cobra.Command{Use: "dashboards"}
	leaf := &cobra.Command{Use: "get <uid>"}
	leaf.Flags().String("output", "text", "Output format")
	root.AddCommand(group)
	group.AddCommand(leaf)
	cause := errors.New("invalid option")

	for _, tc := range []struct {
		name    string
		cmd     *cobra.Command
		message string
		cause   error
		want    string
	}{
		{"command context", leaf, "", cause, "invalid option"},
		{"explicit message", leaf, "  choose another value  ", cause, "choose another value"},
		{"no command", nil, "", cause, "invalid option"},
		{"no message or cause", nil, "  ", nil, "invalid command usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := gcxerrors.NewCommandUsageError(tc.cmd, tc.message, tc.cause)
			assert.Equal(t, tc.want, got.Error())
			assert.Equal(t, tc.cause, errors.Unwrap(got))
			if tc.cause != nil {
				require.ErrorIs(t, got, cause)
			}
			var extracted *gcxerrors.UsageError
			require.ErrorAs(t, fmt.Errorf("caller: %w", got), &extracted)
			assert.Same(t, got, extracted)
			if tc.cmd == nil {
				assert.Empty(t, got.Expected)
				assert.Empty(t, got.Suggestions)
			} else {
				assert.Equal(t, "gcx dashboards get <uid> [flags]", got.Expected)
				assert.Equal(t, []string{"Run 'gcx dashboards get --help' for full usage and examples"}, got.Suggestions)
			}
		})
	}
}
