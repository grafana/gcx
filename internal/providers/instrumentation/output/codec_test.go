package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boolPtr returns a pointer to a bool literal.
//
//nolint:modernize // boolPtr() creates pointer to value, not pointer to type like new()
func boolPtr(b bool) *bool { return &b }

// ─── NormalizeStatus ──────────────────────────────────────────────────────────

func TestNormalizeStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status instrumentation.InstrumentationStatus
		want   string
	}{
		// Shorthand constants (backward compat — types.go values).
		{
			name:   "INSTRUMENTED maps to OK",
			status: instrumentation.StatusInstrumented,
			want:   "OK",
		},
		{
			name:   "INSTRUMENTATION_ERROR maps to FAILING",
			status: instrumentation.StatusError,
			want:   "FAILING",
		},
		{
			name:   "PENDING_INSTRUMENTATION maps to NODATA",
			status: instrumentation.StatusPendingInstrumentation,
			want:   "NODATA",
		},
		{
			name:   "PENDING_UNINSTRUMENTATION maps to NODATA",
			status: instrumentation.StatusPendingUninstrumentation,
			want:   "NODATA",
		},
		{
			name:   "NOT_INSTRUMENTED maps to NODATA",
			status: instrumentation.StatusNotInstrumented,
			want:   "NODATA",
		},
		{
			name:   "EXCLUDED maps to NODATA",
			status: instrumentation.StatusExcluded,
			want:   "NODATA",
		},
		{
			name:   "empty string maps to NODATA",
			status: instrumentation.InstrumentationStatus(""),
			want:   "NODATA",
		},
		// Full proto enum names — K8s monitoring family (wire values from RunK8sMonitoring).
		{
			name:   "K8S_MONITORING_STATUS_INSTRUMENTED maps to OK",
			status: "K8S_MONITORING_STATUS_INSTRUMENTED",
			want:   "OK",
		},
		{
			name:   "K8S_MONITORING_STATUS_ERROR maps to FAILING",
			status: "K8S_MONITORING_STATUS_ERROR",
			want:   "FAILING",
		},
		{
			name:   "K8S_MONITORING_STATUS_NOT_INSTRUMENTED maps to NODATA",
			status: "K8S_MONITORING_STATUS_NOT_INSTRUMENTED",
			want:   "NODATA",
		},
		{
			name:   "K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION maps to NODATA",
			status: "K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION",
			want:   "NODATA",
		},
		{
			name:   "K8S_MONITORING_STATUS_PENDING_UNINSTRUMENTATION maps to NODATA",
			status: "K8S_MONITORING_STATUS_PENDING_UNINSTRUMENTATION",
			want:   "NODATA",
		},
		{
			name:   "K8S_MONITORING_STATUS_EXCLUDED maps to NODATA",
			status: "K8S_MONITORING_STATUS_EXCLUDED",
			want:   "NODATA",
		},
		// Full proto enum names — Discovery item family (wire values from RunK8sDiscovery).
		{
			name:   "INSTRUMENTATION_STATUS_INSTRUMENTED maps to OK",
			status: "INSTRUMENTATION_STATUS_INSTRUMENTED",
			want:   "OK",
		},
		{
			name:   "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR maps to FAILING",
			status: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR",
			want:   "FAILING",
		},
		{
			name:   "INSTRUMENTATION_STATUS_NOT_INSTRUMENTED maps to NODATA",
			status: "INSTRUMENTATION_STATUS_NOT_INSTRUMENTED",
			want:   "NODATA",
		},
		{
			name:   "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION maps to NODATA",
			status: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION",
			want:   "NODATA",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, instroutput.NormalizeStatus(tc.status))
		})
	}
}

// ─── ClusterTableCodec ────────────────────────────────────────────────────────

func TestClusterTableCodec_Format(t *testing.T) {
	t.Parallel()

	assert.Equal(t, format.Format("table"), (&instroutput.ClusterTableCodec{}).Format())
	assert.Equal(t, format.Format("wide"), (&instroutput.ClusterTableCodec{Wide: true}).Format())
}

func TestClusterTableCodec_Decode_ReturnsError(t *testing.T) {
	t.Parallel()

	err := (&instroutput.ClusterTableCodec{}).Decode(strings.NewReader(""), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestClusterTableCodec_Encode_InvalidType(t *testing.T) {
	t.Parallel()

	err := (&instroutput.ClusterTableCodec{}).Encode(&bytes.Buffer{}, "not a slice")
	require.Error(t, err)
}

func TestClusterTableCodec_DefaultColumns(t *testing.T) {
	t.Parallel()

	clusters := []instroutput.ClusterView{
		{
			Name:                  "prod",
			Selection:             "SELECTION_INCLUDED",
			InstrumentationStatus: instrumentation.StatusInstrumented,
			Namespaces:            3,
			Workloads:             10,
			Pods:                  20,
			Nodes:                 5,
			Age:                   "2d",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.ClusterTableCodec{}).Encode(&buf, clusters))
	out := buf.String()

	// Default columns present in headers.
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "NAMESPACES")
	assert.Contains(t, out, "WORKLOADS")
	assert.Contains(t, out, "PODS")
	assert.Contains(t, out, "STATUS")
	assert.NotContains(t, out, "AGE")

	// SELECTION must NOT appear in table output.
	assert.NotContains(t, out, "SELECTION")

	// Wide columns must NOT appear in default table.
	assert.NotContains(t, out, "COST")
	assert.NotContains(t, out, "ENERGY")
	assert.NotContains(t, out, "EVENTS")
	assert.NotContains(t, out, "NODES")

	// STATUS is normalized to OK.
	assert.Contains(t, out, "OK")
	assert.NotContains(t, out, "INSTRUMENTED\t")

	// Data values present.
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "3")
}

func TestClusterTableCodec_DefaultColumns_StatusNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status instrumentation.InstrumentationStatus
		want   string
	}{
		{"instrumented->OK", instrumentation.StatusInstrumented, "OK"},
		{"error->FAILING", instrumentation.StatusError, "FAILING"},
		{"pending->NODATA", instrumentation.StatusPendingInstrumentation, "NODATA"},
		{"not-instrumented->NODATA", instrumentation.StatusNotInstrumented, "NODATA"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clusters := []instroutput.ClusterView{{Name: "c1", InstrumentationStatus: tc.status}}
			var buf bytes.Buffer
			require.NoError(t, (&instroutput.ClusterTableCodec{}).Encode(&buf, clusters))
			assert.Contains(t, buf.String(), tc.want)
		})
	}
}

func TestClusterTableCodec_WideColumns(t *testing.T) {
	t.Parallel()

	clusters := []instroutput.ClusterView{
		{
			Name:                  "prod",
			Selection:             "SELECTION_INCLUDED",
			CostMetrics:           boolPtr(true),  //nolint:modernize
			EnergyMetrics:         boolPtr(false), //nolint:modernize
			ClusterEvents:         boolPtr(true),  //nolint:modernize
			NodeLogs:              boolPtr(false), //nolint:modernize
			InstrumentationStatus: instrumentation.StatusError,
			Namespaces:            2,
			Workloads:             5,
			Pods:                  15,
			Nodes:                 3,
			Age:                   "1h",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.ClusterTableCodec{Wide: true}).Encode(&buf, clusters))
	out := buf.String()

	// Wide adds extra columns.
	assert.Contains(t, out, "COST")
	assert.Contains(t, out, "EVENTS")
	assert.Contains(t, out, "ENERGY")
	assert.Contains(t, out, "LOGS")
	assert.Contains(t, out, "NODES")

	// SELECTION still must NOT appear in wide output.
	assert.NotContains(t, out, "SELECTION")

	// Wide STATUS is raw proto enum, not normalized.
	assert.Contains(t, out, "INSTRUMENTATION_ERROR")
	assert.NotContains(t, out, "FAILING")

	// NAME first, STATUS last.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.NotEmpty(t, lines)
	hdr := lines[0]
	nameIdx := strings.Index(hdr, "NAME")
	costIdx := strings.Index(hdr, "COST")
	statusIdx := strings.Index(hdr, "STATUS")
	assert.Less(t, nameIdx, costIdx, "NAME must come before COST")
	assert.Less(t, costIdx, statusIdx, "COST must come before STATUS")
	assert.NotContains(t, hdr, "AGE")
}

func TestClusterTableCodec_EmptySlice(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.ClusterTableCodec{}).Encode(&buf, []instroutput.ClusterView{}))
	out := buf.String()
	// Headers still rendered even for empty slice.
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "STATUS")
	assert.NotContains(t, out, "AGE")
}

// ─── AppTableCodec ────────────────────────────────────────────────────────────

func TestAppTableCodec_Format(t *testing.T) {
	t.Parallel()

	assert.Equal(t, format.Format("table"), (&instroutput.AppTableCodec{}).Format())
	assert.Equal(t, format.Format("wide"), (&instroutput.AppTableCodec{Wide: true}).Format())
}

func TestAppTableCodec_Decode_ReturnsError(t *testing.T) {
	t.Parallel()

	err := (&instroutput.AppTableCodec{}).Decode(strings.NewReader(""), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestAppTableCodec_Encode_InvalidType(t *testing.T) {
	t.Parallel()

	err := (&instroutput.AppTableCodec{}).Encode(&bytes.Buffer{}, 42)
	require.Error(t, err)
}

func TestAppTableCodec_DefaultColumns(t *testing.T) {
	t.Parallel()

	apps := []instroutput.AppView{
		{
			ClusterName:           "prod",
			Name:                  "payments",
			Autoinstrument:        boolPtr(true), //nolint:modernize
			InstrumentationStatus: instrumentation.StatusInstrumented,
			Workloads:             3,
			Pods:                  9,
			Overrides:             2,
			Age:                   "5h",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.AppTableCodec{}).Encode(&buf, apps))
	out := buf.String()

	// Default columns present in headers.
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "CLUSTER")
	assert.Contains(t, out, "WORKLOADS")
	assert.Contains(t, out, "PODS")
	assert.Contains(t, out, "AUTOINSTRUMENT")
	assert.Contains(t, out, "STATUS")
	assert.NotContains(t, out, "AGE")

	// Wide columns must NOT appear in default table.
	assert.NotContains(t, out, "TRACING")
	assert.NotContains(t, out, "LOGGING")
	assert.NotContains(t, out, "PROCESS_METRICS")
	assert.NotContains(t, out, "EXTENDED_METRICS")
	assert.NotContains(t, out, "PROFILING")
	assert.NotContains(t, out, "OVERRIDES")

	// STATUS normalized.
	assert.Contains(t, out, "OK")

	// Data values.
	assert.Contains(t, out, "payments")
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "true")
}

func TestAppTableCodec_WideColumns(t *testing.T) {
	t.Parallel()

	apps := []instroutput.AppView{
		{
			ClusterName:           "prod",
			Name:                  "payments",
			Autoinstrument:        boolPtr(true),  //nolint:modernize
			Tracing:               boolPtr(true),  //nolint:modernize
			Logging:               boolPtr(false), //nolint:modernize
			ProcessMetrics:        boolPtr(true),  //nolint:modernize
			ExtendedMetrics:       boolPtr(false), //nolint:modernize
			Profiling:             boolPtr(true),  //nolint:modernize
			InstrumentationStatus: instrumentation.StatusPendingInstrumentation,
			Workloads:             3,
			Pods:                  9,
			Overrides:             2,
			Age:                   "5h",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.AppTableCodec{Wide: true}).Encode(&buf, apps))
	out := buf.String()

	// Wide columns present.
	assert.Contains(t, out, "TRACING")
	assert.Contains(t, out, "LOGGING")
	assert.Contains(t, out, "PROCESS_METRICS")
	assert.Contains(t, out, "EXTENDED_METRICS")
	assert.Contains(t, out, "PROFILING")
	assert.Contains(t, out, "OVERRIDES")

	// Wide STATUS is raw proto enum.
	assert.Contains(t, out, "PENDING_INSTRUMENTATION")
	assert.NotContains(t, out, "NODATA")

	// header ordering.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.NotEmpty(t, lines)
	hdr := lines[0]
	nameIdx := strings.Index(hdr, "NAME")
	tracingIdx := strings.Index(hdr, "TRACING")
	statusIdx := strings.Index(hdr, "STATUS")
	assert.Less(t, nameIdx, tracingIdx, "NAME must come before TRACING")
	assert.Less(t, tracingIdx, statusIdx, "TRACING must come before STATUS")
	assert.NotContains(t, hdr, "AGE")
}

// ─── ServiceTableCodec ────────────────────────────────────────────────────────

func TestServiceTableCodec_Format(t *testing.T) {
	t.Parallel()

	assert.Equal(t, format.Format("table"), (&instroutput.ServiceTableCodec{}).Format())
	assert.Equal(t, format.Format("wide"), (&instroutput.ServiceTableCodec{Wide: true}).Format())
}

func TestServiceTableCodec_Decode_ReturnsError(t *testing.T) {
	t.Parallel()

	err := (&instroutput.ServiceTableCodec{}).Decode(strings.NewReader(""), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestServiceTableCodec_Encode_InvalidType(t *testing.T) {
	t.Parallel()

	err := (&instroutput.ServiceTableCodec{}).Encode(&bytes.Buffer{}, struct{}{})
	require.Error(t, err)
}

func TestServiceTableCodec_DefaultColumns(t *testing.T) {
	t.Parallel()

	services := []instroutput.ServiceView{
		{
			ClusterName:           "prod",
			Namespace:             "payments",
			Name:                  "checkout",
			WorkloadType:          "deployment",
			Lang:                  "go",
			InstrumentationStatus: instrumentation.StatusInstrumented,
			Age:                   "3d",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.ServiceTableCodec{}).Encode(&buf, services))
	out := buf.String()

	// Default columns present in headers.
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "CLUSTER")
	assert.Contains(t, out, "NAMESPACE")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "LANG")
	assert.Contains(t, out, "STATUS")
	assert.NotContains(t, out, "AGE")

	// Wide columns must NOT appear.
	assert.NotContains(t, out, "WORKLOAD_TYPE")
	assert.NotContains(t, out, "OS")
	assert.NotContains(t, out, "INSTRUMENTATION_ERROR")

	// STATUS normalized.
	assert.Contains(t, out, "OK")

	// Data values.
	assert.Contains(t, out, "checkout")
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "payments")
	assert.Contains(t, out, "deployment")
	assert.Contains(t, out, "go")
}

func TestServiceTableCodec_DisplayNameFallback(t *testing.T) {
	t.Parallel()

	services := []instroutput.ServiceView{
		{
			ClusterName:      "prod",
			Namespace:        "ns",
			DisplayNamespace: "NS Display",
			Name:             "svc",
			DisplayName:      "SVC Display",
			WorkloadType:     "deployment",
			Lang:             "python",
			Age:              "1h",
		},
		{
			ClusterName:  "prod",
			Namespace:    "ns2",
			Name:         "svc2",
			WorkloadType: "daemonset",
			Lang:         "java",
			Age:          "2h",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.ServiceTableCodec{}).Encode(&buf, services))
	out := buf.String()

	// DisplayName and DisplayNamespace used when set.
	assert.Contains(t, out, "SVC Display")
	assert.Contains(t, out, "NS Display")

	// Fallback to Name/Namespace when Display fields absent.
	assert.Contains(t, out, "svc2")
	assert.Contains(t, out, "ns2")
}

func TestServiceTableCodec_WideColumns(t *testing.T) {
	t.Parallel()

	services := []instroutput.ServiceView{
		{
			ClusterName:                 "prod",
			Namespace:                   "payments",
			Name:                        "checkout",
			WorkloadType:                "deployment",
			OS:                          "linux",
			Lang:                        "go",
			InstrumentationStatus:       instrumentation.StatusError,
			InstrumentationErrorMessage: "agent failed to start",
			Age:                         "3d",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, (&instroutput.ServiceTableCodec{Wide: true}).Encode(&buf, services))
	out := buf.String()

	// Wide columns present.
	assert.Contains(t, out, "OS")
	assert.Contains(t, out, "INSTRUMENTATION_ERROR")
	// TYPE renders WorkloadType; WORKLOAD_TYPE has been dropped to avoid
	// duplicating TYPE (no distinct field exists for a separate column).
	assert.NotContains(t, out, "WORKLOAD_TYPE")

	// Wide STATUS is raw proto enum.
	assert.Contains(t, out, "INSTRUMENTATION_ERROR")
	assert.Contains(t, out, "agent failed to start")

	// header ordering.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.NotEmpty(t, lines)
	hdr := lines[0]
	nameIdx := strings.Index(hdr, "NAME")
	osIdx := strings.Index(hdr, "OS")
	statusIdx := strings.Index(hdr, "STATUS")
	assert.Less(t, nameIdx, osIdx, "NAME must come before OS")
	assert.Less(t, osIdx, statusIdx, "OS must come before STATUS")
	assert.NotContains(t, hdr, "AGE")
}

// ─── JSON/YAML bare-type output ───────────────────────────────────────────────
// JSON/YAML output must be the bare domain type with no
// kind: or apiVersion: fields at the top level.

func TestClusterView_JSON_NoBareEnvelope(t *testing.T) {
	t.Parallel()

	cv := instroutput.ClusterView{
		Name:                  "prod",
		Selection:             "SELECTION_INCLUDED",
		InstrumentationStatus: instrumentation.StatusInstrumented,
		Namespaces:            2,
		Age:                   "1d",
	}

	b, err := json.Marshal(cv)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))

	// no K8s envelope fields.
	assert.NotContains(t, raw, "kind", "kind must not appear in JSON output")
	assert.NotContains(t, raw, "apiVersion", "apiVersion must not appear in JSON output")

	// Selection IS present in JSON (no omitempty).
	assert.Contains(t, raw, "selection", "selection must appear in JSON output")

	// Raw proto enum value for status.
	assert.Equal(t, "INSTRUMENTED", raw["instrumentationStatus"])
}

func TestClusterView_YAML_NoBareEnvelope(t *testing.T) {
	t.Parallel()

	cv := instroutput.ClusterView{
		Name:                  "prod",
		Selection:             "SELECTION_INCLUDED",
		InstrumentationStatus: instrumentation.StatusError,
	}

	b, err := goyaml.Marshal(cv)
	require.NoError(t, err)
	out := string(b)

	// no K8s envelope fields.
	assert.NotContains(t, out, "kind:")
	assert.NotContains(t, out, "apiVersion:")

	// selection IS present in YAML (no omitempty).
	assert.Contains(t, out, "selection:")

	// raw proto enum.
	assert.Contains(t, out, "INSTRUMENTATION_ERROR")
}

func TestAppView_JSON_ClusterNameExcluded(t *testing.T) {
	t.Parallel()

	av := instroutput.AppView{
		ClusterName:           "prod",
		Name:                  "payments",
		Autoinstrument:        boolPtr(true), //nolint:modernize
		InstrumentationStatus: instrumentation.StatusInstrumented,
	}

	b, err := json.Marshal(av)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))

	// ClusterName must NOT appear in JSON (json:"-").
	assert.NotContains(t, raw, "clusterName", "clusterName must not appear in JSON output for AppView")

	// No K8s envelope.
	assert.NotContains(t, raw, "kind")
	assert.NotContains(t, raw, "apiVersion")

	// raw proto enum.
	assert.Equal(t, "INSTRUMENTED", raw["instrumentationStatus"])
}

func TestServiceView_JSON_NoBareEnvelope(t *testing.T) {
	t.Parallel()

	sv := instroutput.ServiceView{
		ClusterName:           "prod",
		Namespace:             "payments",
		Name:                  "checkout",
		WorkloadType:          "deployment",
		InstrumentationStatus: instrumentation.StatusPendingInstrumentation,
	}

	b, err := json.Marshal(sv)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))

	// no K8s envelope fields.
	assert.NotContains(t, raw, "kind")
	assert.NotContains(t, raw, "apiVersion")

	// raw proto enum.
	assert.Equal(t, "PENDING_INSTRUMENTATION", raw["instrumentationStatus"])
}

// ─── Slice-of-views JSON (raw enum in JSON/YAML list) ────────────────────────

func TestClusterView_JSON_SliceRawEnum(t *testing.T) {
	t.Parallel()

	clusters := []instroutput.ClusterView{
		{Name: "c1", Selection: "SELECTION_INCLUDED", InstrumentationStatus: instrumentation.StatusPendingInstrumentation},
		{Name: "c2", Selection: "SELECTION_INCLUDED", InstrumentationStatus: instrumentation.StatusInstrumented},
	}

	b, err := json.Marshal(clusters)
	require.NoError(t, err)

	var raw []map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Len(t, raw, 2)

	assert.Equal(t, "PENDING_INSTRUMENTATION", raw[0]["instrumentationStatus"])
	assert.Equal(t, "INSTRUMENTED", raw[1]["instrumentationStatus"])

	// No K8s envelope fields in any element.
	for _, elem := range raw {
		assert.NotContains(t, elem, "kind")
		assert.NotContains(t, elem, "apiVersion")
	}
}
