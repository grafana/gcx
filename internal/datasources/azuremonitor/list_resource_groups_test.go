package azuremonitor_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/azuremonitor"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListResourceGroupsCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "explicit empty --subscription rejected before any config/datasource I/O",
			args:    []string{"--subscription="},
			wantErr: "--subscription must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &providers.ConfigLoader{}
			cmd := azuremonitor.ListResourceGroupsCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
