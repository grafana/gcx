package query_test

import (
	"bytes"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
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
}

// TestTraceGetCodecDispatch verifies that table and wide codecs route a
// *tempo.GetTraceResponse to the corresponding tempo formatter.
func TestTraceGetCodecDispatch(t *testing.T) {
	newIO := func(format string) *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: format}
		dsquery.RegisterCodecs(ioOpts, true)
		return ioOpts
	}

	// An empty *GetTraceResponse renders only the header line.
	// We verify dispatch by asserting the formatter's signature output.
	resp := &tempo.GetTraceResponse{}

	t.Run("table dispatches to FormatTraceTable", func(t *testing.T) {
		var out bytes.Buffer
		err := newIO("table").Encode(&out, resp)
		require.NoError(t, err)
		assert.Contains(t, out.String(), "spans: 0")
		assert.Contains(t, out.String(), "services: 0")
	})

	t.Run("wide dispatches to FormatTraceWide", func(t *testing.T) {
		var out bytes.Buffer
		err := newIO("wide").Encode(&out, resp)
		require.NoError(t, err)
		assert.Contains(t, out.String(), "spans: 0")
		assert.Contains(t, out.String(), "services: 0")
	})
}
