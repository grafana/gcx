package irm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration(t *testing.T) {
	valid := map[string]time.Duration{
		"7d":    7 * 24 * time.Hour,
		"1d":    24 * time.Hour,
		"90m":   90 * time.Minute,
		"1h30m": 90 * time.Minute,
	}
	for in, want := range valid {
		t.Run("valid "+in, func(t *testing.T) {
			got, err := parseDuration(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	// Text after the day count used to be dropped, so "7dd" and "7dxd"
	// both silently meant seven days.
	for _, in := range []string{"7dd", "7dxd", "7d12h", "1.5d", "d", "xd", ""} {
		t.Run("invalid "+in, func(t *testing.T) {
			_, err := parseDuration(in)
			require.Error(t, err)
		})
	}
}
