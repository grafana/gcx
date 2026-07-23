//nolint:testpackage // Tests verify unexported command constructor wiring.
package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterMetricNames(t *testing.T) {
	names := []string{
		"http_requests_total",
		"http_request_duration_seconds_bucket",
		"app_cartservice_new_carts_created_total",
		"up",
	}

	tests := []struct {
		name     string
		prefix   string
		suffix   string
		contains string
		want     []string
	}{
		{
			name: "no filters returns all",
			want: names,
		},
		{
			name:   "prefix",
			prefix: "http_",
			want:   []string{"http_requests_total", "http_request_duration_seconds_bucket"},
		},
		{
			name:   "suffix",
			suffix: "_total",
			want:   []string{"http_requests_total", "app_cartservice_new_carts_created_total"},
		},
		{
			name:     "contains",
			contains: "cart",
			want:     []string{"app_cartservice_new_carts_created_total"},
		},
		{
			name:     "filters combine with AND",
			prefix:   "http_",
			suffix:   "_total",
			contains: "requests",
			want:     []string{"http_requests_total"},
		},
		{
			name:   "no match yields empty",
			prefix: "nomatch_",
			want:   []string{},
		},
		{
			name:     "case sensitive",
			contains: "CART",
			want:     []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterMetricNames(names, tc.prefix, tc.suffix, tc.contains)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListCmd_Flags(t *testing.T) {
	cmd := listCmd(nil)
	require.Equal(t, "list", cmd.Name())

	for _, name := range []string{"datasource", "match", "prefix", "suffix", "contains", "output"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "missing flag --%s", name)
	}

	// Name filters are provider-specific options and must not have shorthands
	// (docs/design/naming.md 9.4).
	for _, name := range []string{"match", "prefix", "suffix", "contains"} {
		assert.Empty(t, cmd.Flags().Lookup(name).Shorthand, "--%s must not have a shorthand", name)
	}
}
