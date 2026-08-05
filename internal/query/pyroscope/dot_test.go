package pyroscope_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
)

func TestCleanDot(t *testing.T) {
	in := `N1 [label="runtime\nnanotime\ntime_nofake.go:33\n405.99s (91.27%)" id="node1" fontsize=24 shape=box tooltip="runtime.nanotime /usr/local/go/src/runtime/time_nofake.go:33 (405.99s)" color="#b20400" fillcolor="#edd6d5"]`
	got := pyroscope.CleanDot(in)

	assert.NotContains(t, got, "fontsize=")
	assert.NotContains(t, got, `id="node1"`)
	assert.NotContains(t, got, "shape=box")
	assert.NotContains(t, got, `tooltip=`)
	assert.NotContains(t, got, `color="#`)
	assert.Contains(t, got, "nanotime", "function names must survive cleanup")
	assert.Contains(t, got, "time_nofake.go:33", "file:line must survive cleanup")
	assert.Contains(t, got, "405.99s (91.27%)", "values must survive cleanup")
}

func TestDotHasNodes(t *testing.T) {
	assert.True(t, pyroscope.DotHasNodes(`digraph "main" { N1 [label="x"] N1 -> N2 }`))
	assert.False(t, pyroscope.DotHasNodes(`digraph "main" { subgraph cluster_L { "File: main" [label="empty"] } }`))
	assert.False(t, pyroscope.DotHasNodes(""))
}
