package definitions_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestStatusTableGolden(t *testing.T) {
	results := []definitions.StatusResult{
		{Name: "payment-api", UUID: "abc123", Objective: 0.995, Window: "28d", SLI: new(0.9972), Budget: new(0.44), BurnRate: new(1.5), SLI1h: new(0.9991), SLI1d: new(0.998), Status: "OK"},
		{Name: "checkout", UUID: "def456", Objective: 0.999, Window: "7d", SLI: new(0.9985), Budget: new(-0.5), BurnRate: new(0.0), SLI1h: new(0.0), SLI1d: new(1.0), Status: "BREACHING"},
		{Name: "new-service", UUID: "ghi789", Objective: 0.99, Window: "28d", Status: "Creating"},
	}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name string
			rows []definitions.StatusResult
		}{{"populated", results}, {"empty", []definitions.StatusResult{}}} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				codec := definitions.StatusTable().Codec(name)
				require.NoError(t, codec.Encode(&buf, tc.rows))
				testutils.Golden(t, "status_"+name+"_"+tc.name, buf.String())
			})
		}
	}
}
