package kg_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireWriteAPIEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]string
		wantErr bool
	}{
		{name: "enabled true", cfg: map[string]string{"write-api-enabled": "true"}},
		{name: "enabled 1", cfg: map[string]string{"write-api-enabled": "1"}},
		{name: "disabled false", cfg: map[string]string{"write-api-enabled": "false"}, wantErr: true},
		{name: "absent", cfg: nil, wantErr: true},
		// strconv.ParseBool only accepts 1/t/T/TRUE/true/True (and 0/f/... ); the
		// shell-ism "yes"/"on" are NOT truthy and leave the gate closed.
		{name: "yes is not truthy", cfg: map[string]string{"write-api-enabled": "yes"}, wantErr: true},
		{name: "on is not truthy", cfg: map[string]string{"write-api-enabled": "on"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &kg.FakeWriteLoader{ProviderCfg: tt.cfg}
			err := kg.RequireWriteAPIEnabled(context.Background(), loader)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, kg.WriteAPIDisabledMsg, err.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}
