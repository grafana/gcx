package logs_test

import (
	"testing"

	logs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/grafana/gcx/internal/testutils"
)

func TestExemptionsTableGolden(t *testing.T) {
	row := logs.Exemption{ID: "exemption-2", StreamSelector: `{service_name="checkout"}`, Reason: "Keep audit logs", CreatedAt: "2026-09-01T12:00:00Z", ManagedBy: "operator", ExpiresAt: "2026-12-31T00:00:00Z", ActiveInterval: "24h", CreatedBy: "user@example.com", UpdatedAt: "2026-09-02T12:00:00Z"}
	testutils.AssertTableGolden(t, logs.ExemptionsTable(), row, "table", "wide")
}

func TestSegmentsTableGolden(t *testing.T) {
	row := logs.LogSegment{ID: "segment-2", Name: "Checkout", Selector: `{service_name="checkout"}`, FallbackToDefault: true, IsEarly: true, CreatedAt: "2026-09-01T12:00:00Z", UpdatedAt: "2026-09-02T12:00:00Z"}
	testutils.AssertTableGolden(t, logs.SegmentsTable(), row, "table", "wide")
}
