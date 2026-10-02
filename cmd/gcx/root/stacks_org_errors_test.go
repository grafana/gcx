package root_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStackCreateOrganisationUsageProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary")
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing", nil},
		{"empty", []string{"--org", ""}},
		{"blank", []string{"--org", " \t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"cloud", "stacks", "create", "--name", "Demo", "--slug", "demo", "--dry-run"}, tc.args...)
			stdout, code := runGcx(t, args...)
			require.Equal(t, 2, code)
			doc, ok := assertOneJSONValue(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", doc["type"])
			failure, ok := doc["error"].(map[string]any)
			require.True(t, ok)
			assert.EqualValues(t, 2, failure["exitCode"])
			assert.Equal(t, "Invalid command usage", failure["summary"])
			assert.Contains(t, failure["details"], "--org is required")
		})
	}
}
