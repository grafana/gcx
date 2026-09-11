package namedquery

import (
	"testing"
	"time"

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
			name: "numbers are sent as numbers",
			raw:  []string{"frequency=60000", "quantile=0.75"},
			want: map[string]any{"frequency": float64(60000), "quantile": 0.75},
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
