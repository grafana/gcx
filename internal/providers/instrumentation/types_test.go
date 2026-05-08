package instrumentation_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstrumentationStatusConstants verifies that the typed InstrumentationStatus
// constants match the proto enum string values expected by the backend.
func TestInstrumentationStatusConstants(t *testing.T) {
	tests := []struct {
		status instrumentation.InstrumentationStatus
		want   string
	}{
		{instrumentation.StatusPendingInstrumentation, "PENDING_INSTRUMENTATION"},
		{instrumentation.StatusInstrumented, "INSTRUMENTED"},
		{instrumentation.StatusPendingUninstrumentation, "PENDING_UNINSTRUMENTATION"},
		{instrumentation.StatusNotInstrumented, "NOT_INSTRUMENTED"},
		{instrumentation.StatusExcluded, "EXCLUDED"},
		{instrumentation.StatusError, "INSTRUMENTATION_ERROR"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.status))
		})
	}
}

// TestApp_NoSelectionField verifies that App does NOT have a Selection field
// and DOES have an Autoinstrument field.
func TestApp_NoSelectionField(t *testing.T) {
	app := instrumentation.App{
		Name:           "default",
		Autoinstrument: boolPtr(true), //nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
		Tracing:        boolPtr(true), //nolint:modernize
	}

	data, err := json.Marshal(app)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Contains(t, m, "name", "App must serialize name")
	assert.Contains(t, m, "autoInstrument", "App must serialize autoInstrument")
	assert.NotContains(t, m, "selection", "App must NOT have a selection field")
}

// TestApp_TriStateFields verifies that nil *bool fields are omitted from JSON
// (supporting the tri-state "unset = preserve" semantics).
func TestApp_TriStateFields(t *testing.T) {
	tests := []struct {
		name       string
		app        instrumentation.App
		wantFields []string
		wantAbsent []string
	}{
		{
			name:       "all fields nil — only name serialized",
			app:        instrumentation.App{Name: "kube-system"},
			wantFields: []string{"name"},
			wantAbsent: []string{"autoInstrument", "tracing", "logging", "processMetrics", "extendedMetrics", "profiling"},
		},
		{
			name: "autoinstrument true — field present",
			app: instrumentation.App{
				Name:           "default",
				Autoinstrument: boolPtr(true), //nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
			},
			wantFields: []string{"name", "autoInstrument"},
			wantAbsent: []string{"tracing", "logging"},
		},
		{
			name: "autoinstrument false — field present (explicit off)",
			app: instrumentation.App{
				Name:           "default",
				Autoinstrument: boolPtr(false), //nolint:modernize
			},
			wantFields: []string{"name", "autoInstrument"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.app)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))

			for _, f := range tt.wantFields {
				assert.Contains(t, m, f, "field %q should be present", f)
			}
			for _, f := range tt.wantAbsent {
				assert.NotContains(t, m, f, "field %q should be absent", f)
			}
		})
	}
}

// TestCluster_SelectionField verifies that Cluster retains a Selection field
// and that it appears in JSON output even when empty (no omitempty).
func TestCluster_SelectionField(t *testing.T) {
	cluster := instrumentation.Cluster{
		Name:      "prod-1",
		Selection: "SELECTION_INCLUDED",
	}

	data, err := json.Marshal(cluster)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Contains(t, m, "selection", "Cluster MUST have selection field")
	assert.Equal(t, "SELECTION_INCLUDED", m["selection"])
}

// TestCluster_SelectionAlwaysPresent verifies that selection appears even when
// empty (no omitempty — selection is always present in JSON/YAML).
func TestCluster_SelectionAlwaysPresent(t *testing.T) {
	cluster := instrumentation.Cluster{Name: "prod-1"}

	data, err := json.Marshal(cluster)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	_, ok := m["selection"]
	assert.True(t, ok, "selection field must always be present in JSON output (no omitempty)")
}

// TestAppOverride_HasSelection verifies that AppOverride retains a Selection field
// (the per-workload override knob IS used by the backend).
func TestAppOverride_HasSelection(t *testing.T) {
	override := instrumentation.AppOverride{
		Name:      "web-server",
		Selection: "SELECTION_EXCLUDED",
	}

	data, err := json.Marshal(override)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Contains(t, m, "name")
	assert.Contains(t, m, "selection")
	assert.Equal(t, "SELECTION_EXCLUDED", m["selection"])
}

// TestDiscoveryItem_InstrumentationStatus verifies that DiscoveryItem can hold
// the typed InstrumentationStatus and serializes correctly.
func TestDiscoveryItem_InstrumentationStatus(t *testing.T) {
	item := instrumentation.DiscoveryItem{
		ClusterName:           "prod-1",
		Namespace:             "default",
		Name:                  "web",
		WorkloadType:          "deployment",
		InstrumentationStatus: instrumentation.StatusInstrumented,
	}

	data, err := json.Marshal(item)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "INSTRUMENTED", m["instrumentationStatus"])
}

// TestClusterObservedState_FullRoundTrip verifies that ClusterObservedState
// round-trips through JSON correctly.
func TestClusterObservedState_FullRoundTrip(t *testing.T) {
	state := instrumentation.ClusterObservedState{
		Name:                  "prod-1",
		InstrumentationStatus: instrumentation.StatusPendingInstrumentation,
		Namespaces: []instrumentation.NamespaceObservedState{
			{
				Name:                  "default",
				InstrumentationStatus: instrumentation.StatusInstrumented,
				Workloads:             3,
				Pods:                  9,
			},
		},
		Nodes:     5,
		Workloads: 3,
		Pods:      9,
	}

	data, err := json.Marshal(state)
	require.NoError(t, err)

	var got instrumentation.ClusterObservedState
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, state.Name, got.Name)
	assert.Equal(t, state.InstrumentationStatus, got.InstrumentationStatus)
	require.Len(t, got.Namespaces, 1)
	assert.Equal(t, instrumentation.StatusInstrumented, got.Namespaces[0].InstrumentationStatus)
}

// TestCluster_JSONCamelCase verifies that Cluster uses camelCase JSON keys (D.4).
func TestCluster_JSONCamelCase(t *testing.T) {
	tval := true
	c := instrumentation.Cluster{
		Name:          "prod-eu",
		Selection:     "SELECTION_INCLUDED",
		CostMetrics:   &tval,
		ClusterEvents: &tval,
	}
	data, err := json.Marshal(c)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Contains(t, m, "costMetrics", "must use camelCase key costMetrics")
	assert.NotContains(t, m, "costmetrics", "must not use lowercase costmetrics")
	assert.Contains(t, m, "clusterEvents")
	assert.Contains(t, m, "selection")
	assert.Contains(t, m, "name")
}

// TestApp_JSONCamelCase verifies that App uses camelCase JSON keys (D.4).
func TestApp_JSONCamelCase(t *testing.T) {
	tval := true
	a := instrumentation.App{
		Name:            "checkout",
		ProcessMetrics:  &tval,
		ExtendedMetrics: &tval,
		Autoinstrument:  &tval,
	}
	data, err := json.Marshal(a)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Contains(t, m, "processMetrics")
	assert.Contains(t, m, "extendedMetrics")
	assert.Contains(t, m, "autoInstrument")
	assert.NotContains(t, m, "processmetrics")
	assert.NotContains(t, m, "autoinstrument")
}

// TestIsEmptyDefaultCluster verifies that IsEmptyDefaultCluster correctly detects
// a zero-valued Cluster response from the backend (indicating unknown cluster name).
// The backend returns HTTP 200 with a zero-valued proto for unknown identifiers,
// so the client must detect the not-found case itself.
func TestIsEmptyDefaultCluster(t *testing.T) {
	boolFalse := false
	boolTrue := true
	tests := []struct {
		name string
		cl   instrumentation.Cluster
		want bool
	}{
		{
			name: "fully zero-valued — not-found signal",
			cl:   instrumentation.Cluster{Name: "does-not-exist", Selection: ""},
			want: true,
		},
		{
			name: "all flags explicitly false — still empty-default",
			cl: instrumentation.Cluster{
				Name:          "does-not-exist",
				Selection:     "",
				CostMetrics:   &boolFalse,
				EnergyMetrics: &boolFalse,
				ClusterEvents: &boolFalse,
				NodeLogs:      &boolFalse,
			},
			want: true,
		},
		{
			name: "non-empty selection — registered cluster",
			cl:   instrumentation.Cluster{Name: "k3d", Selection: "SELECTION_INCLUDED"},
			want: false,
		},
		{
			name: "one flag true — registered cluster",
			cl: instrumentation.Cluster{
				Name:        "k3d",
				Selection:   "",
				CostMetrics: &boolTrue,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := instrumentation.IsEmptyDefaultCluster(tt.cl)
			if got != tt.want {
				t.Errorf("IsEmptyDefaultCluster() = %v, want %v", got, tt.want)
			}
		})
	}
}

//nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
func boolPtr(b bool) *bool { return &b }
