package athena_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/athena"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatStringList(t *testing.T) {
	t.Run("renders items", func(t *testing.T) {
		var buf bytes.Buffer
		err := athena.FormatStringList(&buf, []string{"alpha", "beta", "gamma"}, "CATALOG")
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "CATALOG")
		assert.Contains(t, out, "alpha")
		assert.Contains(t, out, "gamma")
	})

	t.Run("empty result", func(t *testing.T) {
		var buf bytes.Buffer
		err := athena.FormatStringList(&buf, []string{}, "EMPTY")
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No data")
	})
}
