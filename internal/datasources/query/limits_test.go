package query_test

import (
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

func TestEffectiveLokiLimit(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		format     string
		changed    bool
		wantResult int
	}{
		{
			name:       "explicit limit wins for table",
			limit:      200,
			format:     "table",
			changed:    true,
			wantResult: 200,
		},
		{
			name:       "explicit zero means no limit",
			limit:      0,
			format:     "wide",
			changed:    true,
			wantResult: 0,
		},
		{
			name:       "implicit table uses human default",
			limit:      dsquery.DefaultLokiLimit,
			format:     "table",
			changed:    false,
			wantResult: dsquery.HumanLokiLimit,
		},
		{
			name:       "implicit wide uses human default",
			limit:      dsquery.DefaultLokiLimit,
			format:     "wide",
			changed:    false,
			wantResult: dsquery.HumanLokiLimit,
		},
		{
			name:       "implicit json uses machine default",
			limit:      dsquery.DefaultLokiLimit,
			format:     "json",
			changed:    false,
			wantResult: dsquery.DefaultLokiLimit,
		},
		{
			name:       "implicit yaml uses machine default",
			limit:      dsquery.DefaultLokiLimit,
			format:     "yaml",
			changed:    false,
			wantResult: dsquery.DefaultLokiLimit,
		},
		{
			name:       "implicit raw uses machine default",
			limit:      dsquery.DefaultLokiLimit,
			format:     "raw",
			changed:    false,
			wantResult: dsquery.DefaultLokiLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dsquery.EffectiveLokiLimit(tt.limit, tt.format, tt.changed); got != tt.wantResult {
				t.Fatalf("EffectiveLokiLimit(%d, %q, %t) = %d, want %d", tt.limit, tt.format, tt.changed, got, tt.wantResult)
			}
		})
	}
}
