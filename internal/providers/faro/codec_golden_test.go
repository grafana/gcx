package faro_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// goldenApps covers both a fully populated app and one that leaves every
// optional field empty, so the golden pins the dash placeholders too.
func goldenApps() []faro.FaroApp {
	return []faro.FaroApp{
		{
			ID:                    "42",
			Name:                  "my-app",
			AppKey:                "abc123",
			CollectEndpointURL:    "https://faro.example.com/collect/abc123",
			OTLPIngestEndpointURL: "https://faro.example.com/otlp",
			CORSOrigins:           []faro.CORSOrigin{{URL: "https://app.example.com"}},
			ExtraLogLabels:        map[string]string{"team": "frontend"},
			Settings:              &faro.FaroAppSettings{GeolocationEnabled: true, GeolocationLevel: "country"},
		},
		{
			ID:   "7",
			Name: "minimal-app",
		},
	}
}

func TestAppTableGolden(t *testing.T) {
	for _, name := range []string{"table", "wide"} {
		t.Run(name, func(t *testing.T) {
			codec := faro.AppTable().Codec(name)

			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, toTypedObjs(goldenApps())))

			testutils.Golden(t, "apps_"+name, buf.String())
		})
	}
}

func TestAppTableGoldenEmpty(t *testing.T) {
	codec := faro.AppTable().Codec("table")

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, toTypedObjs(nil)))

	testutils.Golden(t, "apps_table_empty", buf.String())
}
