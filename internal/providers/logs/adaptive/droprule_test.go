package logs_test

import (
	"strings"
	"testing"

	logs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/stretchr/testify/require"
)

func TestReadDropRuleFileSpecFromReader_JSON(t *testing.T) {
	t.Parallel()
	input := `{
  "version": 1,
  "name": "rule-a",
  "body": {
    "drop_rate": 0.5,
    "stream_selector": "{}",
    "levels": ["error"]
  }
}`
	spec, err := logs.ReadDropRuleFileSpecFromReader(strings.NewReader(input))
	require.NoError(t, err)
	require.Equal(t, 1, spec.Version)
	require.Equal(t, "rule-a", spec.Name)
	require.Equal(t, 0.5, spec.Body.DropRate)
	require.Equal(t, "{}", spec.Body.StreamSelector)
	require.Equal(t, []string{"error"}, spec.Body.Levels)
}

func TestReadDropRuleFileSpecFromReader_YAML(t *testing.T) {
	t.Parallel()
	input := `
version: 1
name: rule-yaml
body:
  drop_rate: 10
  stream_selector: '{app="nginx"}'
  levels:
    - warn
`
	spec, err := logs.ReadDropRuleFileSpecFromReader(strings.NewReader(input))
	require.NoError(t, err)
	require.Equal(t, "rule-yaml", spec.Name)
	require.Equal(t, 10.0, spec.Body.DropRate)
	require.Equal(t, `{app="nginx"}`, spec.Body.StreamSelector)
}

func TestReadDropRuleFileSpecFromReader_invalid(t *testing.T) {
	t.Parallel()
	_, err := logs.ReadDropRuleFileSpecFromReader(strings.NewReader("<<<"))
	require.Error(t, err)
}
