package query_test

import (
	"bytes"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphCodecRejectsUnsupportedResponseTypes(t *testing.T) {
	newGraphIO := func() *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: "graph"}
		dsquery.RegisterCodecs(ioOpts, true)
		return ioOpts
	}

	t.Run("rejects loki log stream responses", func(t *testing.T) {
		var out bytes.Buffer
		err := newGraphIO().Encode(&out, &loki.QueryResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "graph output is not supported for log stream queries")
		assert.Contains(t, err.Error(), "gcx logs metrics")
	})

	t.Run("rejects tempo trace search responses", func(t *testing.T) {
		var out bytes.Buffer
		err := newGraphIO().Encode(&out, &tempo.SearchResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "graph output is not supported for trace search results")
	})

	t.Run("rejects infinity query responses", func(t *testing.T) {
		var out bytes.Buffer
		err := newGraphIO().Encode(&out, &infinity.QueryResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Infinity")
	})
}
