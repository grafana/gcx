package azuremonitor_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/azuremonitor"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceGraphQueryCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "empty KQL rejected before any config/datasource I/O",
			args:    []string{"query", "", "--subscription", "sub"},
			wantErr: "KQL query must not be empty",
		},
		{
			name:    "whitespace-only KQL rejected before any config/datasource I/O",
			args:    []string{"query", "   ", "--subscription", "sub"},
			wantErr: "KQL query must not be empty",
		},
		{
			name:    "empty --subscription entry rejected instead of silently reaching the request",
			args:    []string{"query", "Resources | limit 1", "--subscription", ""},
			wantErr: "--subscription entries must not be empty",
		},
		{
			name:    "whitespace-only --subscription entry rejected among valid ones",
			args:    []string{"query", "Resources | limit 1", "--subscription", "sub-a", "--subscription", "   "},
			wantErr: "--subscription entries must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := azuremonitor.ResourceGraphCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
