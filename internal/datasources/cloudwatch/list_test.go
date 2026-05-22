package cloudwatch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudwatch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCmds_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		factory func(*providers.ConfigLoader) *cobra.Command
		args    []string
		wantErr string
	}{
		{
			name:    "list-namespaces missing region",
			factory: cloudwatch.ListNamespacesCmd,
			args:    []string{},
			wantErr: "--region is required",
		},
		{
			name:    "list-metrics missing region",
			factory: cloudwatch.ListMetricsCmd,
			args:    []string{"--namespace", "AWS/EC2"},
			wantErr: "--region is required",
		},
		{
			name:    "list-metrics missing namespace",
			factory: cloudwatch.ListMetricsCmd,
			args:    []string{"--region", "us-east-1"},
			wantErr: "--namespace is required",
		},
		{
			name:    "list-dimensions missing region",
			factory: cloudwatch.ListDimensionsCmd,
			args:    []string{"--namespace", "AWS/EC2", "--metric", "CPUUtilization"},
			wantErr: "--region is required",
		},
		{
			name:    "list-dimensions missing namespace",
			factory: cloudwatch.ListDimensionsCmd,
			args:    []string{"--region", "us-east-1", "--metric", "CPUUtilization"},
			wantErr: "--namespace is required",
		},
		{
			name:    "list-dimensions missing metric",
			factory: cloudwatch.ListDimensionsCmd,
			args:    []string{"--region", "us-east-1", "--namespace", "AWS/EC2"},
			wantErr: "--metric is required",
		},
		{
			name:    "list-accounts missing region",
			factory: cloudwatch.ListAccountsCmd,
			args:    []string{},
			wantErr: "--region is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &providers.ConfigLoader{}
			cmd := tt.factory(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
