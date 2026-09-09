package opensearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validationCase is one "bad flag rejected before any I/O" table entry,
// shared by every leaf command's ValidationErrors test in this package.
type validationCase struct {
	name    string
	args    []string
	wantErr string
}

// runValidationCases drives each case through newCmd with a zero-value
// ConfigLoader, which has no context/config wired up — any code path that
// reaches config loading or datasource resolution fails with an unrelated
// error, so asserting on the specific validation message proves validation
// ran first.
func runValidationCases(t *testing.T, newCmd func(*providers.ConfigLoader) *cobra.Command, tests []validationCase) {
	t.Helper()
	loader := &providers.ConfigLoader{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
