//nolint:testpackage // Tests cover the unexported request builder used by the command.
package traces

import (
	"testing"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMetricsRequest_DefaultInstant(t *testing.T) {
	// No time flags → instant inferred (consistent with metrics query / Prometheus).
	shared := &dsquery.SharedOpts{}
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	req, err := buildMetricsRequest(`{} | rate()`, shared, false, now)
	require.NoError(t, err)

	assert.True(t, req.Instant)
	assert.Equal(t, now.Add(-defaultTraceMetricsWindow), req.Start)
	assert.Equal(t, now, req.End)
	assert.Empty(t, req.Step)
}

func TestBuildMetricsRequest_ExplicitInstantWithDefaultWindow(t *testing.T) {
	shared := &dsquery.SharedOpts{}
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	req, err := buildMetricsRequest(`{} | rate()`, shared, true, now)
	require.NoError(t, err)

	assert.True(t, req.Instant)
	assert.Equal(t, now.Add(-defaultTraceMetricsWindow), req.Start)
	assert.Equal(t, now, req.End)
	assert.Empty(t, req.Step)
}

func TestBuildMetricsRequest_SinceDefaultsToRange(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	shared := &dsquery.SharedOpts{
		TimeRangeOpts: dsquery.TimeRangeOpts{
			From: now.Add(-time.Hour).Format(time.RFC3339),
			To:   now.Format(time.RFC3339),
		},
	}

	req, err := buildMetricsRequest(`{} | rate()`, shared, false, now)
	require.NoError(t, err)

	assert.False(t, req.Instant)
	assert.Equal(t, now.Add(-time.Hour), req.Start)
	assert.Equal(t, now, req.End)
	assert.Equal(t, "60s", req.Step)
}

func TestBuildMetricsRequest_InstantRejectsStep(t *testing.T) {
	shared := &dsquery.SharedOpts{Step: "30s"}
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	_, err := buildMetricsRequest(`{} | rate()`, shared, true, now)
	require.EqualError(t, err, "--step is not supported with --instant")
}

func TestBuildMetricsRequest_InferredInstantRejectsStep(t *testing.T) {
	// --step alone (no time flags) → instant inferred → step rejected.
	shared := &dsquery.SharedOpts{Step: "30s"}
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	_, err := buildMetricsRequest(`{} | rate()`, shared, false, now)
	require.EqualError(t, err, "--step is not supported with --instant")
}

func TestBuildMetricsRequest_ExplicitRange(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour)
	to := now.Add(-30 * time.Minute)
	shared := &dsquery.SharedOpts{
		TimeRangeOpts: dsquery.TimeRangeOpts{
			From: from.Format(time.RFC3339),
			To:   to.Format(time.RFC3339),
		},
		Step: "30s",
	}

	req, err := buildMetricsRequest(`{} | rate()`, shared, false, now)
	require.NoError(t, err)

	assert.False(t, req.Instant)
	assert.Equal(t, from, req.Start)
	assert.Equal(t, to, req.End)
	assert.Equal(t, "30s", req.Step)
}
