package settings //nolint:testpackage

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, name := range []string{"table", "wide"} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				payload *PluginSettings
			}{
				{"full", &PluginSettings{JSONData: PluginJSONData{DefaultLogQueryMode: "loki", MetricsMode: "otel", LogsQueryWithNamespace: "ns_query", LogsQueryWithoutNamespace: "no_ns_query"}}},
				{"defaults", &PluginSettings{}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					opts := &getOpts{}
					flags := pflag.NewFlagSet("get", pflag.ContinueOnError)
					opts.setup(flags)
					require.NoError(t, flags.Set("output", name))
					require.NoError(t, opts.IO.Validate())
					var buf bytes.Buffer
					require.NoError(t, opts.IO.Encode(&buf, tc.payload))
					testutils.Golden(t, "table_"+name+"_"+tc.name, buf.String())
				})
			}
		})
	}
}
