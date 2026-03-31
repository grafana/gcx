package query_test

import (
	"testing"
	"time"

	dsquery "github.com/grafana/gcx/cmd/gcx/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		input    string
		expected time.Time
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: time.Time{},
		},
		{
			name:     "now",
			input:    "now",
			expected: now,
		},
		{
			name:     "now-1h",
			input:    "now-1h",
			expected: now.Add(-time.Hour),
		},
		{
			name:     "now-30m",
			input:    "now-30m",
			expected: now.Add(-30 * time.Minute),
		},
		{
			name:     "now-7d",
			input:    "now-7d",
			expected: now.Add(-7 * 24 * time.Hour),
		},
		{
			name:     "now+1h",
			input:    "now+1h",
			expected: now.Add(time.Hour),
		},
		{
			name:     "RFC3339",
			input:    "2024-01-15T10:30:00Z",
			expected: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:     "Unix timestamp",
			input:    "1705315800",
			expected: time.Unix(1705315800, 0),
		},
		{
			name:    "invalid",
			input:   "invalid-time",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dsquery.ParseTime(tt.input, now)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Unix(), result.Unix())
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "1h",
			input:    "1h",
			expected: time.Hour,
		},
		{
			name:     "30m",
			input:    "30m",
			expected: 30 * time.Minute,
		},
		{
			name:     "15s",
			input:    "15s",
			expected: 15 * time.Second,
		},
		{
			name:     "1h30m",
			input:    "1h30m",
			expected: 90 * time.Minute,
		},
		{
			name:    "invalid",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dsquery.ParseDuration(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
