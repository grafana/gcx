package root_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/util/annotations"
	"github.com/stretchr/testify/require"
)

// Evaluate the actual documented expressions, not copies that can drift away
// from the skill. The fixtures distinguish aggregation, status/absence, and
// instant-window semantics; command-tree validation alone cannot do that.
func TestDebugSkillPromQLExamples(t *testing.T) {
	doc, err := fs.ReadFile(claudeplugin.SkillsFS(), "debug-with-grafana/references/query-patterns.md")
	require.NoError(t, err)

	counter := func(name string, perMinute float64, extra ...string) storage.Series {
		return debugFloatSeries(t, name, func(minute int) float64 {
			return float64(minute) * perMinute
		}, extra...)
	}
	requests := []storage.Series{
		counter("http_requests_total", 90, "instance", "a", "status", "200"),
		counter("http_requests_total", 10, "instance", "a", "status", "500"),
		counter("http_requests_total", 720, "instance", "b", "status", "200"),
		counter("http_requests_total", 180, "instance", "b", "status", "503"),
	}
	classic := []storage.Series{
		counter("http_request_duration_seconds_bucket", 100, "instance", "a", "le", "1"),
		counter("http_request_duration_seconds_bucket", 100, "instance", "a", "le", "2"),
		counter("http_request_duration_seconds_bucket", 100, "instance", "a", "le", "+Inf"),
		counter("http_request_duration_seconds_bucket", 0, "instance", "b", "le", "1"),
		counter("http_request_duration_seconds_bucket", 900, "instance", "b", "le", "2"),
		counter("http_request_duration_seconds_bucket", 900, "instance", "b", "le", "+Inf"),
	}
	failedScrape := counter("up", 0, "instance", "a")

	tests := []struct {
		name    string
		heading string
		series  []storage.Series
		want    map[string]float64
		instant bool
	}{
		{
			name:    "mixed HTTP statuses across instances",
			heading: "HTTP error ratio", series: requests,
			want: map[string]float64{`{job="api"}`: 0.19},
		},
		{
			name:    "missing error series does not silently become zero",
			heading: "HTTP error ratio", series: []storage.Series{requests[0], requests[2]},
			want: map[string]float64{},
		},
		{
			name:    "classic histograms combine populations before quantile",
			heading: "Classic histogram P95", series: classic,
			// 100 fast and 900 slow requests per minute: the 950th falls
			// 850/900 of the way through (1,2], not the mean of two P95s.
			want: map[string]float64{`{job="api"}`: 1 + 850.0/900},
		},
		{
			name:    "native histograms combine without le",
			heading: "Native histogram P95",
			series: []storage.Series{
				debugNativeSeries(t, "a", 100, 0),
				debugNativeSeries(t, "b", 0, 900),
			},
			want: map[string]float64{`{job="api"}`: 1 + 850.0/900},
		},
		{
			name:    "failed scrape retains zero",
			heading: "Scrape status", series: []storage.Series{failedScrape},
			want: map[string]float64{`{__name__="up", instance="a", job="api"}`: 0}, instant: true,
		},
		{
			name:    "missing scrape series is absent",
			heading: "Absent scrape series",
			want:    map[string]float64{`{job="api"}`: 1},
		},
		{
			name:    "failed scrape is not absent",
			heading: "Absent scrape series", series: []storage.Series{failedScrape},
			want: map[string]float64{},
		},
		{
			name:    "window request total is one instant result",
			heading: "Window request total", series: requests,
			want: map[string]float64{`{job="api"}`: 30000}, instant: true,
		},
		{
			name:    "window total handles observed counter reset",
			heading: "Window request total",
			series: []storage.Series{debugFloatSeries(t, "http_requests_total", func(minute int) float64 {
				if minute >= 15 {
					return float64(minute-15) * 10
				}
				return float64(minute) * 10
			}, "instance", "a", "status", "200")},
			// Samples at minutes 1..30: increase=280, extrapolated over
			// 30m rather than the 29m between the first/last sample.
			want: map[string]float64{`{job="api"}`: 280 * 30.0 / 29}, instant: true,
		},
		{
			name:    "window average is not the current gauge",
			heading: "Window gauge average",
			series:  []storage.Series{counter("queue_depth", 1, "instance", "a")},
			// A range vector is left-open: minutes 1..30 average to 15.5.
			want: map[string]float64{`{instance="a", job="api"}`: 15.5}, instant: true,
		},
	}

	engine := promql.NewEngine(promql.EngineOpts{
		MaxSamples: 10000, Timeout: 5 * time.Second, LookbackDelta: 5 * time.Minute,
	})
	t.Cleanup(func() { require.NoError(t, engine.Close()) })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, section, found := strings.Cut(string(doc), "### "+tt.heading+"\n")
			require.True(t, found, "documented query section was removed")
			section, _, _ = strings.Cut(section, "\n##")
			invocations := extractInvocations(section)
			require.Len(t, invocations, 1, "keep one canonical executable example per tested section")

			// Parse real command flags without executing any network calls.
			cmd, args, err := buildRootCmd().Find(invocations[0].args)
			require.NoError(t, err)
			require.Equal(t, "gcx metrics query", cmd.CommandPath())
			require.NoError(t, cmd.ParseFlags(args))
			require.Len(t, cmd.Flags().Args(), 1)
			require.Equal(t, tt.instant, cmd.Flags().Changed("time"))
			require.Equal(t, !tt.instant, cmd.Flags().Changed("from"))
			require.Equal(t, !tt.instant, cmd.Flags().Changed("to"))
			expr := cmd.Flags().Args()[0]

			query, err := engine.NewInstantQuery(t.Context(), debugQueryable(tt.series), nil, expr, time.Unix(1800, 0))
			require.NoError(t, err)
			defer query.Close()
			result := query.Exec(t.Context())
			require.NoError(t, result.Err)
			require.Empty(t, result.Warnings)
			vector, err := result.Vector()
			require.NoError(t, err)
			require.Len(t, vector, len(tt.want))
			for _, sample := range vector {
				want, exists := tt.want[sample.Metric.String()]
				require.True(t, exists, "unexpected output labels: %s", sample.Metric)
				require.InDelta(t, want, sample.F, 1e-6)
			}
		})
	}
}

// Use the real engine with in-memory chunks; importing Prometheus's TSDB test
// runner would also pull cloud/discovery SDKs into this CLI's dependency graph.
func debugFloatSeries(t *testing.T, name string, value func(int) float64, extra ...string) storage.Series {
	t.Helper()
	chunk := chunkenc.NewXORChunk()
	app, err := chunk.Appender()
	require.NoError(t, err)
	for minute := range 31 {
		app.Append(0, int64(minute)*60000, value(minute))
	}
	return &storage.SeriesEntry{
		Lset:             labels.FromStrings(append([]string{"__name__", name, "job", "api"}, extra...)...),
		SampleIteratorFn: chunk.Iterator,
	}
}

func debugNativeSeries(t *testing.T, instance string, fast, slow float64) storage.Series {
	t.Helper()
	chunk := chunkenc.NewFloatHistogramChunk()
	app, err := chunk.Appender()
	require.NoError(t, err)
	for minute := range 31 {
		n := float64(minute)
		h := &histogram.FloatHistogram{
			Schema: histogram.CustomBucketsSchema, CustomValues: []float64{1, 2},
			Count: n * (fast + slow), Sum: n * (fast*0.5 + slow*1.5),
			PositiveSpans:   []histogram.Span{{Offset: 0, Length: 3}},
			PositiveBuckets: []float64{n * fast, n * slow, 0},
		}
		_, _, app, err = app.AppendFloatHistogram(nil, 0, int64(minute)*60000, h, true)
		require.NoError(t, err)
	}
	return &storage.SeriesEntry{
		Lset:             labels.FromStrings("__name__", "http_request_duration_seconds", "job", "api", "instance", instance),
		SampleIteratorFn: chunk.Iterator,
	}
}

func debugQueryable(series []storage.Series) storage.Queryable {
	return &storage.MockQueryable{MockQuerier: &storage.MockQuerier{
		SelectMockFunction: func(_ bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
			var matched []storage.Series
			for _, s := range series {
				if slices.ContainsFunc(matchers, func(m *labels.Matcher) bool { return !m.Matches(s.Labels().Get(m.Name)) }) {
					continue
				}
				matched = append(matched, s)
			}
			slices.SortFunc(matched, func(a, b storage.Series) int { return labels.Compare(a.Labels(), b.Labels()) })
			return &debugSeriesSet{series: matched, index: -1}
		},
	}}
}

type debugSeriesSet struct {
	series []storage.Series
	index  int
}

func (s *debugSeriesSet) Next() bool                      { s.index++; return s.index < len(s.series) }
func (s *debugSeriesSet) At() storage.Series              { return s.series[s.index] }
func (*debugSeriesSet) Err() error                        { return nil }
func (*debugSeriesSet) Warnings() annotations.Annotations { return nil }
