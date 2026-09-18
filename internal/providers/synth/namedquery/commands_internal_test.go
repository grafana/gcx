package namedquery

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseParams(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		want    map[string]any
		wantErr bool
	}{
		{
			name: "strings stay strings",
			raw:  []string{"job=my-check", "instance=https://example.com"},
			want: map[string]any{"job": "my-check", "instance": "https://example.com"},
		},
		{
			// The backend unmarshals frequency into an int and quantile into a
			// float, so sending them quoted fails to decode.
			name: "frequency and quantile are sent as numbers",
			raw:  []string{"frequency=60000", "quantile=0.75"},
			want: map[string]any{"frequency": float64(60000), "quantile": 0.75},
		},
		{
			// job is a string param that can look numeric (e.g. a numeric job
			// ID); only the known-numeric params get coerced.
			name: "a numeric-looking value stays a string outside the numeric allowlist",
			raw:  []string{"job=1234"},
			want: map[string]any{"job": "1234"},
		},
		{
			name:    "a non-numeric value for a numeric param is rejected",
			raw:     []string{"frequency=not-a-number"},
			wantErr: true,
		},
		{
			name: "booleans are sent as booleans",
			raw:  []string{"unsuccessfulOnly=true", "other=false"},
			want: map[string]any{"unsuccessfulOnly": true, "other": false},
		},
		{
			// A URL target contains '=' in its query string; only the first
			// separator may count.
			name: "value may contain equals signs",
			raw:  []string{"instance=https://example.com/?a=b"},
			want: map[string]any{"instance": "https://example.com/?a=b"},
		},
		{
			name: "empty value is allowed",
			raw:  []string{"probe="},
			want: map[string]any{"probe": ""},
		},
		{
			name:    "missing separator is rejected",
			raw:     []string{"job"},
			wantErr: true,
		},
		{
			name:    "missing key is rejected",
			raw:     []string{"=value"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseParams(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func testFrame(values ...any) dataframe.Frame {
	times := make([]any, len(values))
	for i := range values {
		times[i] = float64(i * 1000)
	}
	return dataframe.Frame{
		Schema: dataframe.Schema{
			Fields: []dataframe.Field{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number"},
			},
		},
		Data: dataframe.Data{Values: [][]any{times, values}},
	}
}

// TestPoints_PerFrame proves the fix for the bug where points() only counted
// frame 0's column: it is now frame-scoped, so a probe_execution_rate result
// with 2 probes x 3 samples each is counted per frame (3, 3), not summed or
// taken from one frame only.
func TestPoints_PerFrame(t *testing.T) {
	assert.Equal(t, 3, points(testFrame(10.0, 10.0, 10.0)))
	assert.Equal(t, 3, points(testFrame(20.0, 20.0, 20.0)))
}

// TestTableCodec_MultiSeriesRendersOneRowPerSeries is a codec-level regression
// test: a result with N series (e.g. probe_execution_rate's one series per
// probe) must render N rows with a LABELS column, not collapse to one.
func TestTableCodec_MultiSeriesRendersOneRowPerSeries(t *testing.T) {
	res := Result{
		Query: "probe_execution_rate",
		Series: []Series{
			{Labels: `{probe="canary-us"}`, Value: 10, HasValue: true, Reducible: true, Points: 3},
			{Labels: `{probe="canary-eu"}`, Value: 20, HasValue: true, Reducible: true, Points: 3},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&tableCodec{}).Encode(&buf, res))

	out := buf.String()
	assert.Contains(t, out, "LABELS")
	assert.Contains(t, out, `{probe="canary-us"}`)
	assert.Contains(t, out, `{probe="canary-eu"}`)
}

// TestTableCodec_SingleUnlabeledSeriesMatchesLegacyFormat proves checks_uptime
// -- the single-frame, unlabeled case -- still renders the QUERY/VALUE/POINTS
// rows with no LABELS column, unchanged from before per-frame Series existed.
func TestTableCodec_SingleUnlabeledSeriesMatchesLegacyFormat(t *testing.T) {
	res := Result{
		Query:  "checks_uptime",
		Series: []Series{{Labels: "{}", Value: 0.75, HasValue: true, Reducible: true, Points: 4}},
	}

	var buf bytes.Buffer
	require.NoError(t, (&tableCodec{}).Encode(&buf, res))

	out := buf.String()
	assert.NotContains(t, out, "LABELS")
	assert.Contains(t, out, "QUERY")
	assert.Contains(t, out, "0.7500")
}

func TestParseRange(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		from, to  string
		wantFrom  time.Time
		wantTo    time.Time
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "relative default",
			from:     "now-3h",
			to:       "now",
			wantFrom: now.Add(-3 * time.Hour),
			wantTo:   now,
		},
		{
			name:     "absolute RFC3339",
			from:     "2026-08-05T09:00:00Z",
			to:       "2026-08-05T10:00:00Z",
			wantFrom: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC),
		},
		{
			name:      "inverted range is rejected",
			from:      "now",
			to:        "now-1h",
			wantErr:   true,
			errSubstr: "must be after",
		},
		{
			name:    "zero-width range is rejected",
			from:    "now",
			to:      "now",
			wantErr: true,
		},
		{
			name:    "unparseable duration is rejected",
			from:    "now-bananas",
			to:      "now",
			wantErr: true,
		},
		{
			name:      "bare timestamp is rejected with guidance",
			from:      "yesterday",
			to:        "now",
			wantErr:   true,
			errSubstr: "RFC3339",
		},
		{
			// Prometheus/Explore-style day unit, matching the command's own
			// --help example ("--from now-1d"). time.ParseDuration alone does not
			// understand "d", which is why this must go through shared.ParseTime.
			name:     "day unit is supported",
			from:     "now-1d",
			to:       "now",
			wantFrom: now.Add(-24 * time.Hour),
			wantTo:   now,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := parseRange(tt.from, tt.to, now)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFrom, from)
			assert.Equal(t, tt.wantTo, to)
		})
	}
}
