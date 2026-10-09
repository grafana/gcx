package bigquery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/bigquery"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeTableCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing dataset rejected before any config/datasource I/O",
			args:    []string{"events"},
			wantErr: "dataset is required",
		},
		{
			name:    "dataset in both the table name and --dataset rejected",
			args:    []string{"my_dataset.events", "--dataset", "my_dataset"},
			wantErr: "not both",
		},
		{
			name:    "project in both the table name and --project rejected",
			args:    []string{"my-project.my_dataset.events", "--project", "my-project"},
			wantErr: "not both",
		},
		{
			name:    "invalid --project identifier rejected before any config/datasource I/O",
			args:    []string{"events", "--dataset", "d", "--project", "bad; DROP TABLE x"},
			wantErr: "invalid project",
		},
		{
			name:    "invalid dataset identifier rejected before any config/datasource I/O",
			args:    []string{"events", "--dataset", "bad; DROP TABLE x"},
			wantErr: "invalid dataset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := bigquery.DescribeTableCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
