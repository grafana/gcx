package output_test

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// Each fixture pairs a fully populated row with one leaving the optional
// fields zero, so the goldens pin the blank and normalised renderings too.
// STATUS is the column that differs between formats: normalised in table,
// raw proto enum in wide.

func goldenClusters() []instroutput.ClusterView {
	return []instroutput.ClusterView{
		{
			Name:                  "prod-eu-west",
			Namespaces:            12,
			Workloads:             48,
			Pods:                  310,
			Nodes:                 9,
			CostMetrics:           new(true),
			ClusterEvents:         new(false),
			EnergyMetrics:         new(true),
			NodeLogs:              new(true),
			InstrumentationStatus: instrumentation.StatusInstrumented,
		},
		{
			Name: "bare",
		},
	}
}

func goldenApps() []instroutput.AppView {
	return []instroutput.AppView{
		{
			ClusterName:           "prod-eu-west",
			Name:                  "checkout",
			Workloads:             4,
			Pods:                  20,
			Autoinstrument:        new(true),
			Tracing:               new(true),
			Logging:               new(false),
			ProcessMetrics:        new(true),
			ExtendedMetrics:       new(false),
			Profiling:             new(true),
			InstrumentationStatus: instrumentation.StatusInstrumented,
		},
		{
			ClusterName: "prod-eu-west",
			Name:        "bare",
		},
	}
}

func goldenServices() []instroutput.ServiceView {
	return []instroutput.ServiceView{
		{
			ClusterName:                 "prod-eu-west",
			Namespace:                   "shop",
			Name:                        "cart",
			WorkloadType:                "deployment",
			Lang:                        "go",
			OS:                          "linux",
			InstrumentationStatus:       instrumentation.StatusInstrumented,
			InstrumentationErrorMessage: "",
		},
		{
			ClusterName:  "prod-eu-west",
			Namespace:    "shop",
			Name:         "bare",
			WorkloadType: "daemonset",
		},
	}
}

// Clusters register their narrow codec as "table"; apps and services register
// theirs as "text". The names below mirror the real registrations, because the
// narrow STATUS column selects itself by format name.

func TestClusterTableGolden(t *testing.T) {
	for _, name := range []string{cmdio.FormatTable, cmdio.FormatWide} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, instroutput.ClusterTable().Codec(name).Encode(&buf, goldenClusters()))

			testutils.Golden(t, "clusters_"+name, buf.String())
		})
	}
}

func TestAppTableGolden(t *testing.T) {
	for _, name := range []string{cmdio.FormatText, cmdio.FormatWide} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, instroutput.AppTable().Codec(name).Encode(&buf, goldenApps()))

			testutils.Golden(t, "apps_"+name, buf.String())
		})
	}
}

func TestServiceTableGolden(t *testing.T) {
	for _, name := range []string{cmdio.FormatText, cmdio.FormatWide} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, instroutput.ServiceTable(cmdio.TextOnly).Codec(name).Encode(&buf, goldenServices()))

			testutils.Golden(t, "services_"+name, buf.String())
		})
	}
}

func TestClusterTableGoldenEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, instroutput.ClusterTable().Codec(cmdio.FormatTable).Encode(&buf, []instroutput.ClusterView{}))

	testutils.Golden(t, "clusters_table_empty", buf.String())
}
