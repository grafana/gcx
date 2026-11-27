package graph_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderPercentageBars_LongLabelsCutOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("база", 10) // 40 runes, 80 bytes
	exact := strings.Repeat("я", 30)   // exactly 30 runes, 60 bytes

	var buf bytes.Buffer
	err := graph.RenderPercentageBars(&buf, "", []graph.PercentageBarItem{
		{Name: long, Value: 99, Target: 99.5},
		{Name: exact, Value: 50},
	}, graph.ChartOptions{Width: 120})
	require.NoError(t, err)

	out := buf.String()
	assert.True(t, utf8.ValidString(out), "output must be valid UTF-8")
	assert.Contains(t, out, strings.Repeat("база", 6)+"баз"+"...")
	assert.Contains(t, out, exact, "a label of exactly 30 characters is not shortened")
}

func TestRenderBarChart_LongLabelsCutOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("база", 10)

	var buf bytes.Buffer
	data := &graph.ChartData{Series: []graph.Series{{
		Name:   long,
		Points: []graph.Point{{Value: 3}},
	}}}
	require.NoError(t, graph.RenderBarChart(&buf, data, graph.ChartOptions{Width: 100, Height: 10}))

	out := buf.String()
	assert.True(t, utf8.ValidString(out), "output must be valid UTF-8")
	assert.NotContains(t, out, "�")
}
