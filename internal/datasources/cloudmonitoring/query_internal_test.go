package cloudmonitoring

import (
	"testing"

	"github.com/spf13/pflag"
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

	t.Run("rejects and lists the valid values", func(t *testing.T) {
		for _, r := range []string{"", "   ", "reduce_mean", "REDUCE_AVERAGE", "bogus"} {
			err := validateReducer(r)
			require.Error(t, err, r)
			assert.Contains(t, err.Error(), "--reducer")
			// The whole point of this improvement: an agent or human hitting
			// this error can self-correct from the message alone.
			assert.Contains(t, err.Error(), "REDUCE_MEAN")
			assert.Contains(t, err.Error(), "REDUCE_PERCENTILE_05")
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

	t.Run("rejects and lists the valid values", func(t *testing.T) {
		for _, a := range []string{"", "   ", "align_mean", "ALIGN_AVERAGE", "bogus"} {
			err := validateAligner(a)
			require.Error(t, err, a)
			assert.Contains(t, err.Error(), "--aligner")
			assert.Contains(t, err.Error(), "ALIGN_MEAN")
			assert.Contains(t, err.Error(), "ALIGN_PERCENT_CHANGE")
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

func TestValidateFilters(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		assert.NoError(t, validateFilters(nil))
		assert.NoError(t, validateFilters(map[string]string{"resource.label.zone": "us-east1-b"}))
	})

	t.Run("rejects empty key", func(t *testing.T) {
		err := validateFilters(map[string]string{"": "us-east1-b"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key must not be empty")
	})

	t.Run("rejects empty value", func(t *testing.T) {
		err := validateFilters(map[string]string{"resource.label.zone": ""})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "value must not be empty")
	})
}

// --group-by only takes effect when paired with a real cross-series reducer
// (GCP's Aggregation API ignores groupByFields under REDUCE_NONE), so
// queryOpts.Validate rejects the combination rather than silently returning
// one ungrouped series. Package-internal because the check lives in
// queryOpts.Validate, not one of the exported per-flag validators above.
func TestQueryOptsValidate_GroupByRequiresReducer(t *testing.T) {
	base := func() *queryOpts {
		opts := &queryOpts{}
		opts.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
		opts.Project, opts.Metric = "p", "m"
		return opts
	}

	t.Run("group-by with default REDUCE_NONE rejected", func(t *testing.T) {
		opts := base()
		opts.GroupBys = []string{"resource.label.instance_name"}
		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--group-by has no effect while --reducer is REDUCE_NONE")
	})

	t.Run("group-by with a real reducer is accepted", func(t *testing.T) {
		opts := base()
		opts.GroupBys = []string{"resource.label.instance_name"}
		opts.Reducer = "REDUCE_MEAN"
		assert.NoError(t, opts.Validate())
	})

	t.Run("no group-by is accepted regardless of reducer", func(t *testing.T) {
		opts := base()
		assert.NoError(t, opts.Validate())
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
