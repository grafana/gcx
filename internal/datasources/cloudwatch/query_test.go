package cloudwatch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudwatch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing namespace",
			args:    []string{"--region", "us-east-1", "--metric", "CPUUtilization"},
			wantErr: "--namespace is required",
		},
		{
			name:    "missing metric",
			args:    []string{"--region", "us-east-1", "--namespace", "AWS/EC2"},
			wantErr: "--metric is required",
		},
		{
			name:    "missing region",
			args:    []string{"--namespace", "AWS/EC2", "--metric", "CPUUtilization"},
			wantErr: "--region is required",
		},
		{
			name:    "empty statistic",
			args:    []string{"--region", "us-east-1", "--namespace", "AWS/EC2", "--metric", "CPUUtilization", "--statistic", ""},
			wantErr: "--statistic must not be empty",
		},
		{
			name:    "period zero",
			args:    []string{"--region", "us-east-1", "--namespace", "AWS/EC2", "--metric", "CPUUtilization", "--period", "0"},
			wantErr: "--period must be > 0",
		},
		{
			name:    "since and from mutex",
			args:    []string{"--region", "us-east-1", "--namespace", "AWS/EC2", "--metric", "CPUUtilization", "--since", "1h", "--from", "2026-05-17T08:00:00Z"},
			wantErr: "--since is mutually exclusive with --from",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &providers.ConfigLoader{}
			cmd := cloudwatch.QueryCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
