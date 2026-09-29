package probes_test

import (
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sigsyaml "sigs.k8s.io/yaml"
)

// TestProbesListEnvelope checks that probes list wraps its resource items in
// the list envelope for json and yaml, and that the table stays unchanged.
func TestProbesListEnvelope(t *testing.T) {
	st := &probeAPIState{}
	for i := range 3 {
		st.probes = append(st.probes, probes.Probe{ID: int64(i + 1), Name: fmt.Sprintf("probe-%d", i)})
	}
	srv := newProbeServer(t, st)

	tests := []struct {
		name     string
		args     []string
		wantLen  int
		wantMeta bool
		yaml     bool
	}{
		{name: "json truncated", args: []string{"list", "-o", "json", "--limit", "2"}, wantLen: 2, wantMeta: true},
		{name: "json complete", args: []string{"list", "-o", "json", "--limit", "0"}, wantLen: 3},
		{name: "yaml truncated", args: []string{"list", "-o", "yaml", "--limit", "1"}, wantLen: 1, wantMeta: true, yaml: true},
		{name: "json field selection", args: []string{"list", "--json", "metadata.name", "--limit", "2"}, wantLen: 2, wantMeta: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "synthetic-monitoring", "probes"}, tc.args...)...)
			stdout, stderr, err := runProbes(t, srv.URL, false, "", tc.args...)
			require.NoError(t, err)

			doc := stdout
			if tc.yaml {
				j, err := sigsyaml.YAMLToJSON([]byte(stdout))
				require.NoError(t, err)
				doc = string(j)
			}
			page := testutils.DecodeListPage(t, doc)
			require.Len(t, page.Items, tc.wantLen)
			if tc.wantMeta {
				require.NotNil(t, page.ListMeta)
				assert.Equal(t, tc.wantLen, page.ListMeta.Returned)
				require.NotNil(t, page.ListMeta.Total)
				assert.Equal(t, 3, *page.ListMeta.Total)
				assert.Contains(t, stderr, "showing first")
			} else {
				assert.Nil(t, page.ListMeta)
			}
		})
	}

	t.Run("table unchanged", func(t *testing.T) {
		testutils.PinArgv(t, "gcx", "synthetic-monitoring", "probes", "list", "--limit", "2")
		stdout, stderr, err := runProbes(t, srv.URL, false, "", "list", "--limit", "2")
		require.NoError(t, err)
		assert.Contains(t, stdout, "probe-1")
		assert.NotContains(t, stdout, "probe-2")
		assert.NotContains(t, stdout, "list_meta")
		assert.Contains(t, stderr, "showing first 2 of 3")
	})
}
