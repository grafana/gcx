package root_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitCodesHelpTopic(t *testing.T) {
	for _, args := range [][]string{{"help", "exit-codes"}, {"exit-codes", "--help"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			cmd := root.NewCommandForTest("test", nil)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"--no-color"}, args...))
			require.NoError(t, cmd.Execute())
			assert.Empty(t, stderr.String())
			assert.NotContains(t, stdout.String(), "Unknown help topic")
			for _, info := range gcxerrors.ExitCodes() {
				assert.Contains(t, stdout.String(), fmt.Sprintf("  %d  %s\n", info.Code, info.Name))
				assert.Contains(t, stdout.String(), info.Description)
				assert.Contains(t, stdout.String(), "Example: "+info.Example)
			}
		})
	}
}

func TestExitCodesAdditionalHelpTopic(t *testing.T) {
	cmd := root.NewCommandForTest("test", nil)
	topic, _, err := cmd.Find([]string{"exit-codes"})
	require.NoError(t, err)
	require.Equal(t, "exit-codes", topic.Name())
	assert.True(t, topic.IsAdditionalHelpTopicCommand())
	assert.False(t, topic.Runnable())
	assert.Empty(t, topic.Commands())

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--no-color", "--help"})
	require.NoError(t, cmd.Execute())
	assert.Regexp(t, `Additional help topics:\s+\S+ exit-codes\s+Explain exit codes with example scenarios\.`, stdout.String())
}
