package query_test

import (
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
)

func TestShortExploreRange(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		wantFrom string
		wantTo   string
	}{
		{
			name:     "defaults to short window when neither provided",
			wantFrom: "now-1m",
			wantTo:   "now",
		},
		{
			name:     "preserves explicit relative from",
			from:     "now-1h",
			wantFrom: "now-1h",
			wantTo:   "now",
		},
		{
			name:     "preserves explicit absolute timestamps",
			from:     "2026-04-24T10:00:00Z",
			to:       "2026-04-24T11:00:00Z",
			wantFrom: "2026-04-24T10:00:00Z",
			wantTo:   "2026-04-24T11:00:00Z",
		},
		{
			name:     "trims whitespace from inputs",
			from:     "  now-15m  ",
			to:       "  now  ",
			wantFrom: "now-15m",
			wantTo:   "now",
		},
		{
			name:     "applies short default when only to is provided",
			to:       "now",
			wantFrom: "now-1m",
			wantTo:   "now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFrom, gotTo := dsquery.ShortExploreRange(tt.from, tt.to)
			assert.Equal(t, tt.wantFrom, gotFrom)
			assert.Equal(t, tt.wantTo, gotTo)
		})
	}
}

func TestBuildExploreURL_RejectsNonHTTPSchemes(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{name: "javascript", host: "javascript:alert(1)"},
		{name: "data", host: "data:text/html,<script>alert(1)</script>"},
		{name: "file", host: "file:///etc/passwd"},
		{name: "no scheme", host: "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dsquery.BuildExploreURL(tt.host, 0, map[string]any{"k": "v"}, nil)
			assert.Empty(t, got)
		})
	}
}

func TestBuildExploreURL_AcceptsHTTPAndHTTPS(t *testing.T) {
	for _, host := range []string{"http://localhost:3000", "https://example.grafana.net"} {
		got := dsquery.BuildExploreURL(host, 0, map[string]any{"k": "v"}, nil)
		assert.NotEmpty(t, got, host)
	}
}
