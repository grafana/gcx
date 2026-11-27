package kg

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTimeEpochMs_Since(t *testing.T) {
	valid := map[string]time.Duration{
		"":      time.Hour,
		"30m":   30 * time.Minute,
		"1h30m": 90 * time.Minute,
		"7d":    7 * 24 * time.Hour,
	}
	for since, want := range valid {
		t.Run("valid "+since, func(t *testing.T) {
			start, end, err := resolveTimeEpochMs(since)
			require.NoError(t, err)
			assert.InDelta(t, want.Milliseconds(), end-start, 1)
		})
	}

	// A day count must be a whole number; trailing characters used to be
	// dropped, so "1.5d" and "2abcd" silently meant 1 and 2 days.
	for _, since := range []string{"1.5d", "2abcd", "d", "xd", "7", "1w"} {
		t.Run("invalid "+since, func(t *testing.T) {
			_, _, err := resolveTimeEpochMs(since)
			require.Error(t, err)
		})
	}
}
