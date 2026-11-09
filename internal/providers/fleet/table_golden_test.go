package fleet_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestPipelineTableGolden(t *testing.T) {
	for _, name := range []string{"table", "wide"} {
		t.Run(name, func(t *testing.T) {
			codec := fleet.PipelineTable().Codec(name)
			for _, tt := range []struct {
				name string
				rows []fleet.Pipeline
			}{
				{"populated", []fleet.Pipeline{
					{ID: "42", Name: "otel", Enabled: new(true), ConfigType: "CONFIG_TYPE_OTEL", Matchers: []string{"env=prod", "service=checkout"}},
					{ID: "7", Name: "disabled", Enabled: new(false)},
					{ID: "9", Name: "minimal"},
				}},
				{"empty", []fleet.Pipeline{}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, codec.Encode(&buf, tt.rows))
					testutils.Golden(t, "pipelines_"+name+"_"+tt.name, buf.String())
				})
			}
		})
	}
}
