package instances //nolint:testpackage

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
				payload *InstancesResponse
			}{
				{"rows", &InstancesResponse{Items: []Instance{{Name: "orders", Engine: "postgres", EngineVersion: "17", Environment: "prod", ProviderName: "aws", Namespace: "apps", Host: "db.example.com", InstanceIdentifier: "orders-db", ProviderRegion: "eu-west-1"}, {Name: "empty-fields"}}}},
				{"empty", &InstancesResponse{}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					opts := &listOpts{}
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
