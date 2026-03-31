package query

import (
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedQueryOptsValidateSince(t *testing.T) {
	newOpts := func() *sharedQueryOpts {
		return &sharedQueryOpts{IO: cmdio.Options{OutputFormat: "json"}}
	}

	t.Run("since without to defaults to now", func(t *testing.T) {
		opts := newOpts()
		opts.Since = "1h"

		err := opts.Validate()
		require.NoError(t, err)
		require.NotEmpty(t, opts.From)
		require.NotEmpty(t, opts.To)

		from, err := time.Parse(time.RFC3339, opts.From)
		require.NoError(t, err)
		to, err := time.Parse(time.RFC3339, opts.To)
		require.NoError(t, err)

		assert.WithinDuration(t, to.Add(-time.Hour), from, time.Second)
	})

	t.Run("since with explicit to resolves start relative to end", func(t *testing.T) {
		opts := newOpts()
		opts.Since = "1h"
		opts.To = "2026-03-31T10:00:00Z"

		err := opts.Validate()
		require.NoError(t, err)
		assert.Equal(t, "2026-03-31T09:00:00Z", opts.From)
		assert.Equal(t, "2026-03-31T10:00:00Z", opts.To)
	})

	t.Run("since and from are mutually exclusive", func(t *testing.T) {
		opts := newOpts()
		opts.Since = "1h"
		opts.From = "now-2h"

		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--since is mutually exclusive with --from")
	})

	t.Run("since and window are mutually exclusive", func(t *testing.T) {
		opts := newOpts()
		opts.Since = "1h"
		opts.Window = "1h"

		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--window and --since are mutually exclusive")
	})

	t.Run("invalid since duration rejected", func(t *testing.T) {
		opts := newOpts()
		opts.Since = "tomorrow"

		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --since duration")
	})

	t.Run("invalid to time rejected", func(t *testing.T) {
		opts := newOpts()
		opts.Since = "1h"
		opts.To = "later"

		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --to time")
	})
}
