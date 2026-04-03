package logs_test

import (
	"strings"
	"testing"

	logs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/stretchr/testify/require"
)

const tenMiB = 10 << 20

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
	require.InDelta(t, 0.5, spec.Body.DropRate, 1e-9)
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
	require.InDelta(t, 10.0, spec.Body.DropRate, 1e-9)
	require.Equal(t, `{app="nginx"}`, spec.Body.StreamSelector)
}

func TestReadDropRuleFileSpecFromReader_invalid(t *testing.T) {
	t.Parallel()
	_, err := logs.ReadDropRuleFileSpecFromReader(strings.NewReader("<<<"))
	require.Error(t, err)
}

func TestReadDropRuleFileSpecFromReader_oversized(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("a", tenMiB+1)
	_, err := logs.ReadDropRuleFileSpecFromReader(strings.NewReader(oversized))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum size")
}

func TestValueForJSONFieldDiscovery_emptyRules(t *testing.T) {
	t.Parallel()
	v, ok := logs.ValueForJSONFieldDiscovery(nil).(map[string]any)
	require.True(t, ok)
	require.Contains(t, v, "expires_at")
	require.Contains(t, v, "disabled")
}
