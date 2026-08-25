package azuremonitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAggregation(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, agg := range []string{"Average", "Total", "Maximum", "Minimum", "Count"} {
			assert.NoError(t, validateAggregation(agg), agg)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, agg := range []string{"", "average", "Sum", "AVERAGE", "Averagee"} {
			err := validateAggregation(agg)
			require.Error(t, err, agg)
		}
	})
}

func TestValidateTimeGrain(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, tg := range []string{"auto", "AUTO", "Auto", "PT1M", "PT5M", "PT1H", "P1D", "PT12H"} {
			assert.NoError(t, validateTimeGrain(tg), tg)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		// "5m" is the natural mistake for someone coming from CloudWatch,
		// where --period 5m is legal.
		for _, tg := range []string{"", "5m", "1h", "P", "PT", "PT1X", "not-a-duration"} {
			err := validateTimeGrain(tg)
			require.Error(t, err, tg)
			assert.Contains(t, err.Error(), "--time-grain")
		}
	})
}

func TestValidateTop(t *testing.T) {
	t.Run("empty top is always fine", func(t *testing.T) {
		assert.NoError(t, validateTop("", nil))
		assert.NoError(t, validateTop("", map[string]string{"ApiName": "*"}))
	})

	t.Run("positive integer with dimensions is fine", func(t *testing.T) {
		assert.NoError(t, validateTop("5", map[string]string{"ApiName": "*"}))
	})

	t.Run("rejects non-integer or non-positive values", func(t *testing.T) {
		for _, top := range []string{"0", "-1", "abc", "5.5"} {
			err := validateTop(top, map[string]string{"ApiName": "*"})
			require.Error(t, err, top)
			assert.Contains(t, err.Error(), "--top must be a positive integer")
		}
	})

	t.Run("rejects top without dimensions", func(t *testing.T) {
		err := validateTop("5", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--top is only meaningful together with --dimensions")
	})
}
