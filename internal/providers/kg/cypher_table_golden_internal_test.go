package kg

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestCypherTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct {
		name     string
		entities []CypherEntity
	}{
		{"rows", []CypherEntity{{Type: "Service", Name: "checkout", Scope: map[string]any{"site": "west", "env": "prod", "replicas": 3}}, {Type: "Pod", Name: "worker"}}},
		{"empty", []CypherEntity{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := &cypherOpts{}
			flags := pflag.NewFlagSet("query", pflag.ContinueOnError)
			opts.setup(flags)
			require.NoError(t, flags.Set("output", "table"))
			require.NoError(t, opts.IO.Validate())
			var buf bytes.Buffer
			require.NoError(t, opts.IO.Encode(&buf, &CypherSearchResponse{Entities: tc.entities}))
			testutils.Golden(t, "cypher_table_"+tc.name, buf.String())
		})
	}
}
