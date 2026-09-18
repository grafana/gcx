package loki_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStreamSelector_SimpleSelector(t *testing.T) {
	matchers, lineFilters, ok := loki.ParseStreamSelector(`{app="foo", env!="prod"}`)
	require.True(t, ok)
	assert.Empty(t, lineFilters)
	require.Len(t, matchers, 2)
	assert.Equal(t, loki.LabelMatcher{Key: "app", Operator: "=", Value: "foo"}, matchers[0])
	assert.Equal(t, loki.LabelMatcher{Key: "env", Operator: "!=", Value: "prod"}, matchers[1])
	assert.True(t, matchers[0].Inclusive())
	assert.False(t, matchers[1].Inclusive())
}

func TestParseStreamSelector_WithLineFilters(t *testing.T) {
	matchers, lineFilters, ok := loki.ParseStreamSelector(`{app="foo"} |= "error" != "debug"`)
	require.True(t, ok)
	require.Len(t, matchers, 1)
	require.Len(t, lineFilters, 2)
	assert.Equal(t, loki.LineFilter{Operator: "|=", Value: "error"}, lineFilters[0])
	assert.Equal(t, loki.LineFilter{Operator: "!=", Value: "debug"}, lineFilters[1])
}

func TestParseStreamSelector_RegexMatchers(t *testing.T) {
	matchers, _, ok := loki.ParseStreamSelector(`{app=~"foo.*", env!~"prod.*"}`)
	require.True(t, ok)
	require.Len(t, matchers, 2)
	assert.Equal(t, "=~", matchers[0].Operator)
	assert.Equal(t, "!~", matchers[1].Operator)
}

func TestParseStreamSelector_UnsupportedConstructs(t *testing.T) {
	tests := []string{
		`{app="foo"} | json`,
		`{app="foo"} | logfmt`,
		`rate({app="foo"}[5m])`,
		`sum(count_over_time({app="foo"}[5m]))`,
		`{app="foo"} | label_format newlabel=oldlabel`,
		``,
		`not a selector at all`,
	}
	for _, expr := range tests {
		_, _, ok := loki.ParseStreamSelector(expr)
		assert.False(t, ok, "expected expr %q to be unsupported", expr)
	}
}

func TestParseStreamSelector_EmptySelectorBody(t *testing.T) {
	_, _, ok := loki.ParseStreamSelector(`{}`)
	assert.False(t, ok)
}
