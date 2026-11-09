package probes_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestProbeTableGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []probes.Probe
	}{
		{"populated", []probes.Probe{
			{ID: 42, Name: "public-probe", Region: "us-west-2", Public: true, Online: true},
			{ID: 43, Name: "private probe", Region: "eu-west-1"},
		}},
		{"empty", []probes.Probe{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, probes.ProbeTable().Codec("table").Encode(&buf, tc.rows))
			testutils.Golden(t, "probes_table_"+tc.name, buf.String())
		})
	}
}
