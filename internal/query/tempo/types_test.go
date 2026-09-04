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
	zero, two := 0, 2
	result := &tempo.BaselineResult{
		SeedTraceID:      "seed",
		SeedPartial:      true,
		SeedSpanCount:    12,
		SeedServiceCount: 3,
		Query:            "{ }",
		Candidates: []tempo.BaselineCandidate{
			{TraceID: "candidate-a", RootServiceName: "checkout", ErrorCount: &zero},
			{TraceID: "candidate-b", RootServiceName: "checkout", ErrorCount: &two},
			{TraceID: "candidate-c", RootServiceName: "checkout"},
		},
		ListMeta: &cmdio.ListMeta{Truncated: true, Returned: 3},
	}

	var out bytes.Buffer
	require.NoError(t, cmdio.NewFieldSelectCodec([]string{"traceID", "errorCount"}).Encode(&out, result))

	assert.JSONEq(t, `{
		"seedTraceID": "seed",
		"seedPartial": true,
		"seedSpanCount": 12,
		"seedServiceCount": 3,
		"query": "{ }",
		"candidates": [
			{"traceID": "candidate-a", "errorCount": 0},
			{"traceID": "candidate-b", "errorCount": 2},
			{"traceID": "candidate-c", "errorCount": null}
		],
		"list_meta": {"truncated": true, "returned": 3}
	}`, out.String())
}
