package bigquery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/bigquery"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListDatasetsCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "invalid --project identifier rejected before any config/datasource I/O",
			args:    []string{"--project", "bad; DROP TABLE x"},
			wantErr: "invalid project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := bigquery.ListDatasetsCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
