package shared_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
