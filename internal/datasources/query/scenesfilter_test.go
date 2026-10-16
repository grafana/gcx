package query_test

import (
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/datasources/query/scenesfiltertest"
	"github.com/stretchr/testify/assert"
)

func TestEscapeScenesFilterField_RoundTripsThroughScenesDecoder(t *testing.T) {
	values := []string{"plain", "a,b", "a#b", "foo|bar", "a,b#c|d", "", `quote"d`, "with space", "ünïcode"}
	for _, v := range values {
		escaped, ok := dsquery.EscapeScenesFilterField(v)
		assert.True(t, ok, v)

		got := scenesfiltertest.Decode("key|=|" + escaped)
		assert.Equal(t, scenesfiltertest.Filter{Key: "key", Operator: "=", Value: v}, got, v)
	}
}

func TestEscapeScenesFilterField_RejectsLiteralEscapeTokens(t *testing.T) {
	for _, v := range []string{"x__gfp__y", "x__gfc__y", "x__gfh__y"} {
		_, ok := dsquery.EscapeScenesFilterField(v)
		assert.False(t, ok, v)
	}
}

func TestDecode_UnescapedDelimitersChangeTheValue(t *testing.T) {
	assert.Equal(t, "a", scenesfiltertest.Decode("k|=|a,b").Value)
	assert.Equal(t, "a", scenesfiltertest.Decode("k|=|a#b").Value)
}
