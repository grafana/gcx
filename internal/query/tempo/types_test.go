package tempo_test

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaselineResultJSONFieldSelectionTargetsCandidates(t *testing.T) {
	result := &tempo.BaselineResult{
		SeedTraceID:      "seed",
		SeedPartial:      true,
		SeedSpanCount:    12,
		SeedServiceCount: 3,
		Query:            "{ }",
		Candidates: []tempo.BaselineCandidate{
			{TraceID: "candidate-a", RootServiceName: "checkout"},
			{TraceID: "candidate-b", RootServiceName: "checkout"},
		},
		ListMeta: &cmdio.ListMeta{Truncated: true, Returned: 2},
	}

	var out bytes.Buffer
	require.NoError(t, cmdio.NewFieldSelectCodec([]string{"traceID"}).Encode(&out, result))

	assert.JSONEq(t, `{
		"seedTraceID": "seed",
		"seedPartial": true,
		"seedSpanCount": 12,
		"seedServiceCount": 3,
		"query": "{ }",
		"candidates": [
			{"traceID": "candidate-a"},
			{"traceID": "candidate-b"}
		],
		"list_meta": {"truncated": true, "returned": 2}
	}`, out.String())
}
