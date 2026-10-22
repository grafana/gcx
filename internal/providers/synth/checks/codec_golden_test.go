package checks_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// goldenChecks pairs a fully populated check with one that leaves the optional
// fields zero, so the goldens pin the unknown-type and zero-probe renderings.
func goldenChecks() []checks.Check {
	return []checks.Check{
		{
			ID:        42,
			Job:       "shop-frontend",
			Target:    "https://shop.example.com",
			Frequency: 60000,
			Timeout:   3000,
			Enabled:   true,
			Settings:  checks.CheckSettings{"http": map[string]any{}},
			Probes:    []int64{1, 2, 3},
		},
		{
			Job:       "bare job",
			Target:    "tcp://db.example.com:5432",
			Frequency: 120000,
			Timeout:   10000,
			Settings:  checks.CheckSettings{},
		},
	}
}

func TestCheckTableGolden(t *testing.T) {
	for _, name := range []string{"table", "wide"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, checks.CheckTable().Codec(name).Encode(&buf, goldenChecks()))

			testutils.Golden(t, "checks_"+name, buf.String())
		})
	}
}

func TestCheckTableGoldenEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, checks.CheckTable().Codec("table").Encode(&buf, []checks.Check{}))

	testutils.Golden(t, "checks_table_empty", buf.String())
}
