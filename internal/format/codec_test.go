package format_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLCodec_ExponentFloatsRoundTripAsNumbers(t *testing.T) {
	in := map[string]any{
		"small":    1e-07,
		"large":    1e21,
		"negative": -2.5e-10,
		"plain":    3.0,
		"text":     "1e-07",
	}

	var buf bytes.Buffer
	require.NoError(t, format.NewYAMLCodec().Encode(&buf, in))

	var out map[string]any
	require.NoError(t, newPromotingCodec().Decode(&buf, &out))

	assert.InDelta(t, 1e-07, out["small"], 0)
	assert.InDelta(t, 1e21, out["large"], 0)
	assert.InDelta(t, -2.5e-10, out["negative"], 0)
	assert.EqualValues(t, 3, out["plain"])
	assert.Equal(t, "1e-07", out["text"], "a quoted string must stay a string")
}

func TestYAMLCodec_DecodeExponentFloatEdgeCases(t *testing.T) {
	var out map[string]any
	src := "1e3: key\nquoted: '1e-07'\nsingle: 1E+2\nlist:\n  - 1e-07\n  - e5\n"
	require.NoError(t, newPromotingCodec().Decode(strings.NewReader(src), &out))

	assert.Equal(t, "key", out["1e3"], "mapping keys stay strings")
	assert.Equal(t, "1e-07", out["quoted"])
	assert.InDelta(t, 100.0, out["single"], 0)
	assert.Equal(t, []any{1e-07, "e5"}, out["list"])
}

func TestYAMLCodec_DecodeEmptyInputIsEOF(t *testing.T) {
	var out map[string]any
	err := newPromotingCodec().Decode(strings.NewReader(""), &out)
	assert.ErrorIs(t, err, io.EOF)
}

func newPromotingCodec() *format.YAMLCodec {
	c := format.NewYAMLCodec()
	c.PromoteExponentFloats = true

	return c
}

func TestYAMLCodec_DefaultKeepsExponentStrings(t *testing.T) {
	var out map[string]any
	require.NoError(t, format.NewYAMLCodec().Decode(strings.NewReader("sha: 4385e12\nbig: 1e10\n"), &out))

	assert.Equal(t, "4385e12", out["sha"])
	assert.Equal(t, "1e10", out["big"])
}

func TestYAMLCodec_BlockScalarsStayStrings(t *testing.T) {
	for _, indicator := range []string{"|", "|-", ">", ">-"} {
		var out map[string]any
		src := "a: " + indicator + "\n  1e5\nb: 1e5\n"
		require.NoError(t, newPromotingCodec().Decode(strings.NewReader(src), &out), indicator)

		assert.IsType(t, "", out["a"], indicator)
		assert.InDelta(t, 1e5, out["b"], 0, indicator)
	}
}
