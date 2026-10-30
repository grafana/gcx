package traces_test

import (
	"testing"

	traces "github.com/grafana/gcx/internal/providers/traces/adaptive"
	"github.com/grafana/gcx/internal/testutils"
)

func TestRecommendationTableGolden(t *testing.T) {
	row := traces.Recommendation{ID: "rec-2", Message: "Reduce noisy traces", Tags: []string{"volume", "cost"}, Applied: true, Dismissed: true, Stale: true, CreatedAt: "2026-09-01T12:00:00Z", Actions: []traces.RecommendationAction{{}, {}}}
	testutils.AssertTableGolden(t, traces.RecommendationTable(), row, "table", "wide")
}

func TestPolicyTableGolden(t *testing.T) {
	row := traces.Policy{ID: "policy-2", Name: "Sample 10%", Type: "probabilistic", ExpiresAt: "2026-12-31T00:00:00Z", VersionCreatedBy: "user@example.com", VersionCreatedAt: "2026-09-01T12:00:00Z"}
	testutils.AssertTableGolden(t, traces.PolicyTable(), row, "table", "wide")
}
