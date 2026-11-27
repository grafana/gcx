package resources //nolint:testpackage // Exercise the unexported pull options.

import (
	"io"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPullFetchRequestCompletesListing(t *testing.T) {
	opts := &pullOpts{}
	require.True(t, opts.fetchRequest(config.NamespacedRESTConfig{}, io.Discard).CompleteListing)
}
