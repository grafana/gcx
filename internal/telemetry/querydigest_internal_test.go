package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetCapturedQuery(t *testing.T) {
	t.Helper()
	capturedQuery = ""
	t.Cleanup(func() { capturedQuery = "" })
}

func TestQueryDigestEmptyWithoutCapture(t *testing.T) {
	resetCapturedQuery(t)
	assert.Empty(t, QueryDigest())
}

func TestQueryDigestDeterministicAndTruncated(t *testing.T) {
	resetCapturedQuery(t)

	CaptureQuery(`up{job="grafana"}`)
	d1 := QueryDigest()
	require.Len(t, d1, digestHexLen)

	CaptureQuery(`up{job="grafana"}`)
	assert.Equal(t, d1, QueryDigest(), "same query must digest identically")
}

func TestQueryDigestDiffersByQuery(t *testing.T) {
	resetCapturedQuery(t)

	CaptureQuery("up")
	a := QueryDigest()
	CaptureQuery("down")
	assert.NotEqual(t, a, QueryDigest(), "different queries must digest differently")
}

func TestCaptureQueryTrimsWhitespace(t *testing.T) {
	resetCapturedQuery(t)

	CaptureQuery("  up  ")
	trimmed := QueryDigest()
	CaptureQuery("up")
	assert.Equal(t, QueryDigest(), trimmed, "surrounding whitespace must not change the digest")
}

func TestCaptureQueryIgnoresBlank(t *testing.T) {
	resetCapturedQuery(t)

	CaptureQuery("   ")
	assert.Empty(t, QueryDigest(), "a blank expression must not be captured")
}
