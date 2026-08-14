package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecommendationsList_JSONPromotedFieldOnEmptyPage pins the promoted keys
// of MetricRecommendation, which embeds MetricRule with a yaml tag only.
// encoding/json writes the fields of the embedded struct into the parent
// object, so "metric" is a top-level key. The type walk once read the name of
// the embedded type as the key, so the command rejected "metric" on an empty
// page and offered "MetricRule.metric", which is no key at all.
func TestRecommendationsList_JSONPromotedFieldOnEmptyPage(t *testing.T) {
	loader := newContractLoader(t, fakeMetricsAPI("[]"))

	res := runAdaptiveCmd(t, loader, false, "recommendations", "list", "--json", "metric,managed_by")

	require.NoError(t, res.err, "stderr: %s", res.stderr)
	assert.JSONEq(t, `[]`, res.stdout)
}

// TestRecommendationsList_JSONUnknownPathIsRejected is the other half: a path
// that no promoted key matches must still fail.
func TestRecommendationsList_JSONUnknownPathIsRejected(t *testing.T) {
	loader := newContractLoader(t, fakeMetricsAPI("[]"))

	res := runAdaptiveCmd(t, loader, false, "recommendations", "list", "--json", "MetricRule")

	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "unknown field(s) in --json: MetricRule")
	assert.Empty(t, res.stdout, "a rejected selection must write nothing")
}
