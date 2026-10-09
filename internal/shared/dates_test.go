package shared_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime_NumericMagnitudeDetection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{name: "seconds (10 digits)", input: "1705315800", want: time.Unix(1705315800, 0)},
		{name: "milliseconds (13 digits)", input: "1705315800123", want: time.Unix(1705315800, 123*int64(time.Millisecond))},
		{name: "microseconds (16 digits)", input: "1705315800123456", want: time.Unix(1705315800, 123456*int64(time.Microsecond))},
		{name: "nanoseconds (19 digits)", input: "1705315800123456789", want: time.Unix(0, 1705315800123456789)},
		{
			name:  "fractional seconds with full nanosecond precision",
			input: "1705315800.123456789",
			want:  time.Unix(1705315800, 123456789),
		},
		{
			name:  "fractional seconds padded to nanoseconds",
			input: "1705315800.5",
			want:  time.Unix(1705315800, 500000000),
		},
		{
			name:  "fractional seconds truncated beyond nanosecond precision",
			input: "1705315800.1234567891",
			want:  time.Unix(1705315800, 123456789),
		},
		{
			name:  "negative timestamp before the epoch",
			input: "-100",
			want:  time.Unix(-100, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shared.ParseTime(tt.input, time.Now())
			require.NoError(t, err)
			assert.True(t, got.Equal(tt.want), "ParseTime(%q) = %v (UnixNano=%d), want %v (UnixNano=%d)",
				tt.input, got, got.UnixNano(), tt.want, tt.want.UnixNano())
		})
	}
}

// TestParseTime_NanosecondPrecisionSurvivesFloat64Range pins the actual bug
// fix: a float64 round trip can't exactly represent a 19-digit nanosecond
// epoch value (float64 only has ~15-17 significant digits), so the old
// strconv.ParseFloat-based implementation silently corrupted it.
func TestParseTime_NanosecondPrecisionSurvivesFloat64Range(t *testing.T) {
	const input = "1764021019123456789"
	want := int64(1764021019123456789)

	got, err := shared.ParseTime(input, time.Now())
	require.NoError(t, err)
	assert.Equal(t, want, got.UnixNano(), "nanosecond value was not preserved exactly")
}

func TestParseTime_RejectsTooManyDigits(t *testing.T) {
	_, err := shared.ParseTime("17050315800123456789123", time.Now())
	require.Error(t, err)
}

func TestHasSubMillisecondPrecision(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "RFC3339", input: "2024-01-15T10:30:00Z", want: false},
		{name: "relative", input: "now-1h", want: false},
		{name: "seconds", input: "1705315800", want: false},
		{name: "milliseconds", input: "1705315800123", want: false},
		{name: "microseconds", input: "1705315800123456", want: true},
		{name: "nanoseconds", input: "1705315800123456789", want: true},
		{name: "fractional seconds, millisecond precision", input: "1705315800.123", want: false},
		{name: "fractional seconds, microsecond precision", input: "1705315800.123456", want: true},
		{name: "fractional seconds, trailing zeros beyond ms don't count", input: "1705315800.1230000", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shared.HasSubMillisecondPrecision(tt.input))
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  string
	}{
		{name: "empty string parses to zero", input: "", expected: 0},
		{name: "go-style hours", input: "1h", expected: time.Hour},
		{name: "compound big to small", input: "1h30m", expected: 90 * time.Minute},
		{name: "calendar days", input: "7d", expected: 7 * 24 * time.Hour},
		{name: "calendar weeks", input: "2w", expected: 2 * 7 * 24 * time.Hour},
		{name: "calendar years", input: "1y", expected: 365 * 24 * time.Hour},
		{name: "compound days and hours", input: "1d12h", expected: 36 * time.Hour},
		{name: "negative preserved", input: "-1h", expected: -time.Hour},
		{name: "zero without unit", input: "0", expected: 0},
		{name: "fractional rejected", input: "1.5h", wantErr: "valid units"},
		{name: "reversed order rejected", input: "30m1h", wantErr: "valid units"},
		{name: "unparseable reports accepted units", input: "tomorrow", wantErr: "valid units: s, m, h, d, w, y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shared.ParseDuration(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
