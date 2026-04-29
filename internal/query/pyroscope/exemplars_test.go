package pyroscope_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func num(i int64) json.Number { return json.Number(strconv.FormatInt(i, 10)) }

func TestBuildProfileExemplarsResult(t *testing.T) {
	from := time.Unix(1000, 0).UTC()
	to := time.Unix(2000, 0).UTC()
	profileType := "process_cpu:cpu:nanoseconds:cpu:nanoseconds"

	resp := &pyroscope.SelectSeriesResponse{
		Series: []pyroscope.TimeSeries{
			{
				Labels: []pyroscope.LabelPair{
					{Name: "service_name", Value: "frontend"},
					{Name: "__period_type__", Value: "cpu"}, // internal label should be filtered
				},
				Points: []pyroscope.TimePoint{
					{
						Value:     num(1_000_000),
						Timestamp: num(1500000),
						Exemplars: []pyroscope.Exemplar{
							{ProfileID: "p-1", Timestamp: num(1500100), Value: num(5_000_000), SpanID: "span-1"},
							{ProfileID: "p-2", Timestamp: num(1500200), Value: num(10_000_000), SpanID: "span-2"},
							{ProfileID: "p-3", Timestamp: num(1500300), Value: num(1_000_000), SpanID: "span-3"},
						},
					},
				},
			},
			{
				Labels: []pyroscope.LabelPair{
					{Name: "service_name", Value: "backend"},
				},
				Points: []pyroscope.TimePoint{
					{
						Value:     num(500_000),
						Timestamp: num(1600000),
						Exemplars: []pyroscope.Exemplar{
							{ProfileID: "p-4", Timestamp: num(1600100), Value: num(20_000_000), SpanID: "span-4"},
						},
					},
				},
			},
		},
	}

	t.Run("flatten sort and truncate", func(t *testing.T) {
		result := pyroscope.BuildProfileExemplarsResult(resp, from, to, profileType, 3)

		assert.Equal(t, from, result.From)
		assert.Equal(t, to, result.To)
		assert.Equal(t, profileType, result.ProfileType)
		assert.Len(t, result.Exemplars, 3, "top 3 of 4 exemplars")

		// Sorted by value desc.
		assert.Equal(t, "p-4", result.Exemplars[0].ProfileID)
		assert.Equal(t, int64(20_000_000), result.Exemplars[0].Value)
		assert.Equal(t, "p-2", result.Exemplars[1].ProfileID)
		assert.Equal(t, "p-1", result.Exemplars[2].ProfileID)

		// Internal labels filtered from merged labels.
		for _, e := range result.Exemplars {
			_, hasInternal := e.Labels["__period_type__"]
			assert.False(t, hasInternal, "internal __..__ labels must be filtered from output")
		}

		// Series labels are merged into exemplar labels.
		assert.Equal(t, "backend", result.Exemplars[0].Labels["service_name"])
		assert.Equal(t, "frontend", result.Exemplars[1].Labels["service_name"])
	})

	t.Run("no truncation when topN <= 0", func(t *testing.T) {
		result := pyroscope.BuildProfileExemplarsResult(resp, from, to, profileType, 0)
		assert.Len(t, result.Exemplars, 4)
	})

	t.Run("empty response produces empty list", func(t *testing.T) {
		result := pyroscope.BuildProfileExemplarsResult(&pyroscope.SelectSeriesResponse{}, from, to, profileType, 10)
		assert.Empty(t, result.Exemplars)
	})
}

func TestBuildSpanExemplarsResult(t *testing.T) {
	from := time.Unix(1000, 0).UTC()
	to := time.Unix(2000, 0).UTC()
	profileType := "process_cpu:cpu:nanoseconds:cpu:nanoseconds"

	resp := &pyroscope.SelectHeatmapResponse{
		Series: []pyroscope.HeatmapSeries{
			{
				Labels: []pyroscope.LabelPair{
					{Name: "service_name", Value: "frontend"},
				},
				Slots: []pyroscope.HeatmapSlot{
					{
						Timestamp: num(1500000),
						Exemplars: []pyroscope.Exemplar{
							{SpanID: "span-1", Timestamp: num(1500100), Value: num(5_000_000)},
							{SpanID: "", Timestamp: num(1500200), Value: num(50_000_000)}, // empty span, dropped
							{SpanID: "span-3", Timestamp: num(1500300), Value: num(15_000_000)},
						},
					},
				},
			},
		},
	}

	t.Run("skips empty span and sorts by value desc", func(t *testing.T) {
		result := pyroscope.BuildSpanExemplarsResult(resp, from, to, profileType, 10)

		assert.Len(t, result.Exemplars, 2, "entries without span ID should be skipped")
		assert.Equal(t, "span-3", result.Exemplars[0].SpanID)
		assert.Equal(t, int64(15_000_000), result.Exemplars[0].Value)
		assert.Equal(t, "span-1", result.Exemplars[1].SpanID)
	})

	t.Run("truncates to topN", func(t *testing.T) {
		result := pyroscope.BuildSpanExemplarsResult(resp, from, to, profileType, 1)
		assert.Len(t, result.Exemplars, 1)
		assert.Equal(t, "span-3", result.Exemplars[0].SpanID)
	})
}

func TestTopCardinalityLabelNames(t *testing.T) {
	tests := []struct {
		name   string
		input  []map[string]string
		n      int
		want   []string
		notIn  []string
		wantEq bool
	}{
		{
			name:   "empty input",
			input:  nil,
			n:      3,
			want:   nil,
			wantEq: true,
		},
		{
			name: "n=0 hides all",
			input: []map[string]string{
				{"service": "a"},
			},
			n:      0,
			want:   nil,
			wantEq: true,
		},
		{
			name: "picks highest-cardinality labels",
			input: []map[string]string{
				{"service": "a", "region": "eu", "version": "v1"},
				{"service": "b", "region": "eu", "version": "v1"},
				{"service": "c", "region": "us", "version": "v1"},
			},
			// service=3 distinct, region=2, version=1.
			n:      2,
			want:   []string{"service", "region"},
			wantEq: true,
		},
		{
			name: "internal labels excluded",
			input: []map[string]string{
				{"service": "a", "__name__": "cpu"},
				{"service": "b", "__name__": "cpu"},
			},
			n:     3,
			notIn: []string{"__name__"},
		},
		{
			name: "tie broken alphabetically",
			input: []map[string]string{
				{"alpha": "1", "zulu": "1"},
				{"alpha": "2", "zulu": "2"},
			},
			n:      2,
			want:   []string{"alpha", "zulu"},
			wantEq: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pyroscope.TopCardinalityLabelNames(tc.input, tc.n)
			if tc.wantEq {
				assert.Equal(t, tc.want, got)
			}
			for _, forbidden := range tc.notIn {
				assert.NotContains(t, got, forbidden)
			}
		})
	}
}

func TestFormatProfileExemplarsTable(t *testing.T) {
	t.Run("empty result prints placeholder", func(t *testing.T) {
		var buf bytes.Buffer
		result := &pyroscope.ProfileExemplarsResult{
			ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
		}
		require.NoError(t, pyroscope.FormatProfileExemplarsTable(&buf, result, 3))
		assert.Contains(t, buf.String(), "(no exemplars)")
	})

	t.Run("renders rows with span id when present", func(t *testing.T) {
		var buf bytes.Buffer
		result := &pyroscope.ProfileExemplarsResult{
			ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
			Exemplars: []pyroscope.ProfileExemplar{
				{ProfileID: "p-1", Timestamp: time.UnixMilli(1700000000000).UTC(), Value: 1000, SpanID: "span-1", Labels: map[string]string{"service_name": "frontend"}},
			},
		}
		require.NoError(t, pyroscope.FormatProfileExemplarsTable(&buf, result, 3))
		out := buf.String()
		assert.Contains(t, out, "p-1")
		assert.Contains(t, out, "span-1")
		assert.Contains(t, out, "SERVICE_NAME")
	})
}

func TestFormatSpanExemplarsTable(t *testing.T) {
	t.Run("empty result prints placeholder", func(t *testing.T) {
		var buf bytes.Buffer
		result := &pyroscope.SpanExemplarsResult{
			ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
		}
		require.NoError(t, pyroscope.FormatSpanExemplarsTable(&buf, result, 3))
		assert.Contains(t, buf.String(), "(no span exemplars)")
	})
}
