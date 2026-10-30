package resources //nolint:testpackage // Tests the shared human codecs and their structured input.

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
)

func TestBatchSkipReasons(t *testing.T) {
	for _, action := range []string{"pushed", "deleted"} {
		t.Run(action, func(t *testing.T) {
			summary := &remote.OperationSummary{}
			summary.RecordSkipped("read-only")
			summary.RecordSkipped("not server-verified")
			reasons := summary.SkippedReasons()
			reasons["read-only"] = 99
			require.Equal(t, 1, summary.SkippedReasons()["read-only"])
			result := batchMutationFromSummary(action, summary, true)
			var text bytes.Buffer
			require.NoError(t, (&mutationSummaryCodec{}).Encode(&text, result))
			require.Contains(t, text.String(), "1 skipped: read-only")
			require.Contains(t, text.String(), "1 skipped: not server-verified")
			data, err := json.Marshal(result)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(data, &decoded))
			counts, ok := decoded["summary"].(map[string]any)
			require.True(t, ok)
			require.InDelta(t, 2, counts["skipped"], 0)
			require.Equal(t, map[string]any{"read-only": float64(1), "not server-verified": float64(1)}, decoded["skipped_reasons"])
		})
	}
	require.Equal(t, "2 resource types skipped: not listable; 1 skipped: read-only", formatSkipReasons(map[string]int{"read-only": 1, "not listable": 2}))
}
