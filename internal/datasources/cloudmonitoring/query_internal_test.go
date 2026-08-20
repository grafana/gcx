package cloudmonitoring

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateReducer(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, r := range []string{
			"REDUCE_NONE", "REDUCE_MEAN", "REDUCE_MIN", "REDUCE_MAX", "REDUCE_SUM",
			"REDUCE_STDDEV", "REDUCE_COUNT", "REDUCE_COUNT_TRUE", "REDUCE_COUNT_FALSE",
			"REDUCE_FRACTION_TRUE", "REDUCE_PERCENTILE_99", "REDUCE_PERCENTILE_95",
			"REDUCE_PERCENTILE_50", "REDUCE_PERCENTILE_05",
		} {
			assert.NoError(t, validateReducer(r), r)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, r := range []string{"", "   ", "reduce_mean", "REDUCE_AVERAGE", "bogus"} {
			err := validateReducer(r)
			require.Error(t, err, r)
			assert.Contains(t, err.Error(), "--reducer")
		}
	})
}

func TestValidateAligner(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, a := range []string{
			"ALIGN_NONE", "ALIGN_DELTA", "ALIGN_RATE", "ALIGN_INTERPOLATE", "ALIGN_NEXT_OLDER",
			"ALIGN_MIN", "ALIGN_MAX", "ALIGN_MEAN", "ALIGN_COUNT", "ALIGN_SUM", "ALIGN_STDDEV",
			"ALIGN_COUNT_TRUE", "ALIGN_COUNT_FALSE", "ALIGN_FRACTION_TRUE",
			"ALIGN_PERCENTILE_99", "ALIGN_PERCENTILE_95", "ALIGN_PERCENTILE_50", "ALIGN_PERCENTILE_05",
			"ALIGN_PERCENT_CHANGE",
		} {
			assert.NoError(t, validateAligner(a), a)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, a := range []string{"", "   ", "align_mean", "ALIGN_AVERAGE", "bogus"} {
			err := validateAligner(a)
			require.Error(t, err, a)
			assert.Contains(t, err.Error(), "--aligner")
		}
	})
}

func TestValidateAlignmentPeriod(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, p := range []string{"", "+60s", "+300s", "+3600s", "+86400s"} {
			assert.NoError(t, validateAlignmentPeriod(p), p)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, p := range []string{"   ", "60s", "60", "+60m", "+60", "auto"} {
			err := validateAlignmentPeriod(p)
			require.Error(t, err, p)
			assert.Contains(t, err.Error(), "--alignment-period")
		}
	})
}

func TestValidateGroupBys(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		assert.NoError(t, validateGroupBys(nil))
		assert.NoError(t, validateGroupBys([]string{"resource.label.instance_name"}))
		assert.NoError(t, validateGroupBys([]string{"resource.label.instance_name", "metric.label.response_code"}))
	})

	t.Run("rejects empty or whitespace-only entries", func(t *testing.T) {
		for _, groupBys := range [][]string{
			{""},
			{"   "},
			{"resource.label.instance_name", ""},
			{"resource.label.instance_name", "   "},
		} {
			err := validateGroupBys(groupBys)
			require.Error(t, err, groupBys)
			assert.Contains(t, err.Error(), "--group-by entries must not be empty")
		}
	})
}
