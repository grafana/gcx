package metrics

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSegmentsTableCodec_TruncatesSelectorOnRuneBoundary(t *testing.T) {
	selector := `{team="` + strings.Repeat("я", 70) + `"}`
	segments := []MetricSegment{{ID: "seg-1", Name: "ru", Selector: selector}}

	var buf bytes.Buffer
	require.NoError(t, (&segmentsTableCodec{}).Encode(&buf, segments))

	out := buf.String()
	assert.True(t, utf8.ValidString(out), "table output must be valid UTF-8")
	assert.Contains(t, out, `{team="`+strings.Repeat("я", 50)+"...")
	assert.NotContains(t, out, selector)

	buf.Reset()
	require.NoError(t, (&segmentsTableCodec{wide: true}).Encode(&buf, segments))
	assert.Contains(t, buf.String(), selector, "wide output keeps the full selector")
}
