//nolint:testpackage // White-box tests cover labels option resolution.
package pyroscope

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPyroscopeLabelsOptsResolveExpr(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		args    []string
		want    string
		wantErr string
	}{
		{
			name: "no selector is valid",
			want: "",
		},
		{
			name: "positional argument",
			args: []string{`{service_name="frontend"}`},
			want: `{service_name="frontend"}`,
		},
		{
			name: "expr flag",
			expr: `{namespace="prod"}`,
			want: `{namespace="prod"}`,
		},
		{
			name:    "both positional and flag rejected",
			expr:    `{namespace="prod"}`,
			args:    []string{`{service_name="frontend"}`},
			wantErr: "not both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &pyroscopeLabelsOpts{Expr: tt.expr}
			got, err := opts.resolveExpr(tt.args)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
