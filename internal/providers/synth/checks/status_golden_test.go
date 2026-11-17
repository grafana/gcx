package checks_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestStatusTableGolden(t *testing.T) {
	results := []checks.CheckStatusResult{
		{ID: 42, Job: "Shop Frontend", Target: "https://shop.example.com", Type: "http", Success: new(0.9972), LatencyMs: new(250.4), ProbesUp: 2, ProbesTotal: 3, ProbeNames: []string{"Oregon", "Paris (offline)"}, Status: "OK"},
		{ID: 43, Job: "ping-check", Target: "db.example.com", Type: "ping", Success: new(0.0), LatencyMs: new(1107.6), ProbesTotal: 2, Status: "FAILING"},
		{Job: "new check", Target: "https://new.example.com", Type: "http", ProbesTotal: 1, Status: "NODATA"},
	}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name string
			rows []checks.CheckStatusResult
		}{{"populated", results}, {"empty", []checks.CheckStatusResult{}}} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				codec := checks.StatusTable().Codec(name)
				require.NoError(t, codec.Encode(&buf, tc.rows))
				testutils.Golden(t, "status_"+name+"_"+tc.name, buf.String())
			})
		}
	}
}
