package query

import (
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedQueryOptsValidate(t *testing.T) {
	newOpts := func() *sharedQueryOpts {
		return &sharedQueryOpts{IO: cmdio.Options{OutputFormat: "json"}}
	}

	assertRange := func(t *testing.T, from, to string, want time.Duration) {
		t.Helper()

		parsedFrom, err := time.Parse(time.RFC3339, from)
		require.NoError(t, err)
		parsedTo, err := time.Parse(time.RFC3339, to)
		require.NoError(t, err)

		assert.WithinDuration(t, parsedTo.Add(-want), parsedFrom, time.Second)
	}

	tests := []struct {
		name    string
		setup   func(*sharedQueryOpts)
		wantErr string
		assert  func(*testing.T, *sharedQueryOpts)
	}{
		{
			name: "since without to defaults to now",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "1h"
			},
			assert: func(t *testing.T, opts *sharedQueryOpts) {
				t.Helper()
				require.NotEmpty(t, opts.From)
				require.NotEmpty(t, opts.To)
				assertRange(t, opts.From, opts.To, time.Hour)
			},
		},
		{
			name: "since with explicit to resolves start relative to end",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "1h"
				opts.To = "2026-03-31T10:00:00Z"
			},
			assert: func(t *testing.T, opts *sharedQueryOpts) {
				t.Helper()
				assert.Equal(t, "2026-03-31T09:00:00Z", opts.From)
				assert.Equal(t, "2026-03-31T10:00:00Z", opts.To)
			},
		},
		{
			name: "since and from are mutually exclusive",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "1h"
				opts.From = "now-2h"
			},
			wantErr: "--since is mutually exclusive with --from",
		},
		{
			name: "invalid since duration rejected",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "tomorrow"
			},
			wantErr: "invalid --since duration",
		},
		{
			name: "invalid to time rejected",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "1h"
				opts.To = "later"
			},
			wantErr: "invalid --to time",
		},
		{
			name: "negative since rejected",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "-1h"
			},
			wantErr: "--since must be greater than 0",
		},
		{
			name: "zero since rejected",
			setup: func(opts *sharedQueryOpts) {
				opts.Since = "0"
			},
			wantErr: "--since must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := newOpts()
			if tt.setup != nil {
				tt.setup(opts)
			}

			err := opts.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.assert != nil {
				tt.assert(t, opts)
			}
		})
	}
}
