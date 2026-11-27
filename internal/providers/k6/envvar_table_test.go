package k6_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvVarTable_TruncatesValueOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("я", 50)
	vars := []k6.EnvVar{
		{ID: 1, Name: "RU_LONG", Value: long},
		{ID: 2, Name: "RU_EXACT", Value: strings.Repeat("я", 40)},
	}

	var buf bytes.Buffer
	require.NoError(t, k6.EnvVarTable().Codec("table").Encode(&buf, vars))

	out := buf.String()
	assert.True(t, utf8.ValidString(out), "table output must be valid UTF-8")
	assert.Contains(t, out, strings.Repeat("я", 37)+"...")
	assert.NotContains(t, out, long)
	assert.Contains(t, out, strings.Repeat("я", 40), "a value of exactly 40 characters is not truncated")
}
