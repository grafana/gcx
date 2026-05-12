//nolint:testpackage // white-box testing: accesses unexported run function and source interfaces.
package status

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test fakes ───────────────────────────────────────────────────────────────

// fakeMonitoringSource implements monitoringSource using a fixed slice.
type fakeMonitoringSource struct {
	clusters []instrumentation.ClusterObservedState
	err      error
}

func (f *fakeMonitoringSource) RunK8sMonitoring(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
	return f.clusters, f.err
}

// fakePipelineSource implements pipelineSource using a fixed slice.
type fakePipelineSource struct {
	pipelines []instrumentation.Pipeline
	err       error
}

func (f *fakePipelineSource) ListPipelines(_ context.Context) ([]instrumentation.Pipeline, error) {
	return f.pipelines, f.err
}

// fakeDiscoverySource implements discoverySource using a fixed slice.
type fakeDiscoverySource struct {
	items []instrumentation.DiscoveryItem
	err   error
}

func (f *fakeDiscoverySource) RunK8sDiscovery(_ context.Context) ([]instrumentation.DiscoveryItem, error) {
	return f.items, f.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// k8sPipeline builds a K8s monitoring pipeline fixture for a cluster name.
func k8sPipeline(id, clusterName string) instrumentation.Pipeline {
	return instrumentation.Pipeline{
		ID:   id,
		Name: "k8s-monitoring-" + clusterName,
		Metadata: map[string]any{
			"type":    "k8s_monitoring",
			"cluster": clusterName,
		},
	}
}

// ─── Cluster-view tests ───────────────────────────────────────────────────────

func TestRunClusterView(t *testing.T) {
	tests := []struct {
		name          string
		clusterFilter string
		monClusters   []instrumentation.ClusterObservedState
		pipes         []instrumentation.Pipeline
		monErr        error
		pipeErr       error
		wantErr       bool
		wantNames     []string // expected cluster names; nil means empty result
		wantChecks    func(t *testing.T, got []instroutput.ClusterView)
	}{
		{
			// cluster enumeration includes pre-Alloy clusters that have
			// been Set but whose Alloy collector has not yet started reporting.
			name:        "pre-Alloy cluster appears with StatusPendingInstrumentation",
			monClusters: nil, // RunK8sMonitoring returns nothing
			pipes:       []instrumentation.Pipeline{k8sPipeline("p1", "new-cluster")},
			wantNames:   []string{"new-cluster"},
			wantChecks: func(t *testing.T, got []instroutput.ClusterView) {
				t.Helper()
				require.Len(t, got, 1)
				assert.Equal(t, "new-cluster", got[0].Name)
				assert.Equal(t, instrumentation.StatusPendingInstrumentation, got[0].InstrumentationStatus)
				// Pre-Alloy cluster: no observed metrics.
				assert.Equal(t, 0, got[0].Namespaces)
				assert.Equal(t, 0, got[0].Nodes)
				assert.Equal(t, 0, got[0].Workloads)
				assert.Equal(t, 0, got[0].Pods)
			},
		},
		{
			// cluster visible in RunK8sMonitoring gets its status and metrics.
			name: "instrumented cluster surfaces with observed metrics",
			monClusters: []instrumentation.ClusterObservedState{
				{
					Name:                  "my-cluster",
					InstrumentationStatus: instrumentation.StatusInstrumented,
					Nodes:                 3,
					Workloads:             5,
					Pods:                  10,
					Namespaces: []instrumentation.NamespaceObservedState{
						{Name: "ns1"},
						{Name: "ns2"},
					},
				},
			},
			pipes:     nil,
			wantNames: []string{"my-cluster"},
			wantChecks: func(t *testing.T, got []instroutput.ClusterView) {
				t.Helper()
				require.Len(t, got, 1)
				assert.Equal(t, instrumentation.StatusInstrumented, got[0].InstrumentationStatus)
				assert.Equal(t, 3, got[0].Nodes)
				assert.Equal(t, 5, got[0].Workloads)
				assert.Equal(t, 10, got[0].Pods)
				assert.Equal(t, 2, got[0].Namespaces)
			},
		},
		{
			// cluster present in both monitoring and pipelines keeps
			// monitoring-side status (BothSources via enumerate).
			name: "cluster in both sources uses monitoring-side status",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "shared-cluster", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipes:     []instrumentation.Pipeline{k8sPipeline("p1", "shared-cluster")},
			wantNames: []string{"shared-cluster"},
			wantChecks: func(t *testing.T, got []instroutput.ClusterView) {
				t.Helper()
				require.Len(t, got, 1)
				assert.Equal(t, instrumentation.StatusInstrumented, got[0].InstrumentationStatus)
			},
		},
		{
			// --cluster flag narrows the result to a single cluster.
			name:          "--cluster filter returns only that cluster",
			clusterFilter: "cluster-a",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "cluster-a", InstrumentationStatus: instrumentation.StatusInstrumented},
				{Name: "cluster-b", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipes:     nil,
			wantNames: []string{"cluster-a"},
		},
		{
			// non-existent cluster name yields an empty result.
			name:          "--cluster filter for unknown cluster returns empty",
			clusterFilter: "no-such-cluster",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "cluster-a", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipes:     nil,
			wantNames: nil,
		},
		{
			name:      "empty monitoring and pipelines returns empty slice",
			wantNames: nil,
		},
		{
			name:    "monitoring RPC error is propagated",
			monErr:  errors.New("monitoring RPC failed"),
			wantErr: true,
		},
		{
			name:    "pipeline RPC error is propagated",
			pipeErr: errors.New("pipeline RPC failed"),
			wantErr: true,
		},
		{
			// Alphabetical sort is delegated to enumerate; verify via multi-cluster case.
			name: "results are sorted alphabetically by cluster name",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "zebra", InstrumentationStatus: instrumentation.StatusInstrumented},
				{Name: "alpha", InstrumentationStatus: instrumentation.StatusInstrumented},
				{Name: "moose", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipes:     nil,
			wantNames: []string{"alpha", "moose", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mon := &fakeMonitoringSource{clusters: tt.monClusters, err: tt.monErr}
			pipe := &fakePipelineSource{pipelines: tt.pipes, err: tt.pipeErr}
			disc := &fakeDiscoverySource{} // not used in cluster-level path

			result, err := run(context.Background(), tt.clusterFilter, "", mon, pipe, disc)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			views, ok := result.([]instroutput.ClusterView)
			require.True(t, ok, "expected []ClusterView, got %T", result)

			var gotNames []string
			for _, v := range views {
				gotNames = append(gotNames, v.Name)
			}
			assert.Equal(t, tt.wantNames, gotNames)

			if tt.wantChecks != nil {
				tt.wantChecks(t, views)
			}
		})
	}
}

// ─── Service-view tests ───────────────────────────────────────────────────────

func TestRunServiceView(t *testing.T) {
	tests := []struct {
		name            string
		clusterFilter   string
		namespaceFilter string
		items           []instrumentation.DiscoveryItem
		discErr         error
		wantErr         bool
		wantNames       []string // expected workload names; nil means empty result
		wantChecks      func(t *testing.T, got []instroutput.ServiceView)
	}{
		{
			// --namespace switches the command to workload-level output.
			name:            "--namespace switches to service view and filters by namespace",
			namespaceFilter: "my-ns",
			items: []instrumentation.DiscoveryItem{
				{
					ClusterName:           "c1",
					Namespace:             "my-ns",
					Name:                  "svc1",
					InstrumentationStatus: instrumentation.StatusInstrumented,
				},
				{
					ClusterName:           "c1",
					Namespace:             "other-ns",
					Name:                  "svc2",
					InstrumentationStatus: instrumentation.StatusNotInstrumented,
				},
			},
			wantNames: []string{"svc1"},
		},
		{
			// --cluster + --namespace combine as AND filters.
			name:            "--cluster and --namespace both applied",
			clusterFilter:   "cluster-a",
			namespaceFilter: "my-ns",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "cluster-a", Namespace: "my-ns", Name: "svc1"},
				{ClusterName: "cluster-b", Namespace: "my-ns", Name: "svc2"},
				{ClusterName: "cluster-a", Namespace: "other-ns", Name: "svc3"},
			},
			wantNames: []string{"svc1"},
		},
		{
			name:            "ServiceView fields are populated correctly",
			namespaceFilter: "my-ns",
			items: []instrumentation.DiscoveryItem{
				{
					ClusterName:           "c1",
					Namespace:             "my-ns",
					Name:                  "worker",
					WorkloadType:          "deployment",
					DisplayNamespace:      "My Namespace",
					DisplayName:           "Worker Service",
					OS:                    "linux",
					Lang:                  "go",
					InstrumentationStatus: instrumentation.StatusInstrumented,
				},
			},
			wantNames: []string{"worker"},
			wantChecks: func(t *testing.T, got []instroutput.ServiceView) {
				t.Helper()
				require.Len(t, got, 1)
				v := got[0]
				assert.Equal(t, "c1", v.ClusterName)
				assert.Equal(t, "my-ns", v.Namespace)
				assert.Equal(t, "worker", v.Name)
				assert.Equal(t, "deployment", v.WorkloadType)
				assert.Equal(t, "My Namespace", v.DisplayNamespace)
				assert.Equal(t, "Worker Service", v.DisplayName)
				assert.Equal(t, "linux", v.OS)
				assert.Equal(t, "go", v.Lang)
				assert.Equal(t, instrumentation.StatusInstrumented, v.InstrumentationStatus)
			},
		},
		{
			name:            "no matching namespace returns empty slice",
			namespaceFilter: "no-such-ns",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "my-ns", Name: "svc1"},
			},
			wantNames: nil,
		},
		{
			name:            "discovery RPC error is propagated",
			namespaceFilter: "my-ns",
			discErr:         errors.New("discovery RPC failed"),
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mon := &fakeMonitoringSource{} // not used in service-view path
			pipe := &fakePipelineSource{}  // not used in service-view path
			disc := &fakeDiscoverySource{items: tt.items, err: tt.discErr}

			result, err := run(context.Background(), tt.clusterFilter, tt.namespaceFilter, mon, pipe, disc)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			views, ok := result.([]instroutput.ServiceView)
			require.True(t, ok, "expected []ServiceView, got %T", result)

			var gotNames []string
			for _, v := range views {
				gotNames = append(gotNames, v.Name)
			}
			assert.Equal(t, tt.wantNames, gotNames)

			if tt.wantChecks != nil {
				tt.wantChecks(t, views)
			}
		})
	}
}

// TestRun_ReturnsRawSlices verifies that the run() function still returns raw
// []ClusterView / []ServiceView (not envelopes). The command.go caller is
// responsible for wrapping in the canonical envelope before encoding.
func TestRun_ReturnsRawSlices(t *testing.T) {
	t.Run("cluster view returns []ClusterView", func(t *testing.T) {
		mon := &fakeMonitoringSource{
			clusters: []instrumentation.ClusterObservedState{
				{Name: "c1", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
		}
		pipe := &fakePipelineSource{}
		disc := &fakeDiscoverySource{}

		result, err := run(context.Background(), "", "", mon, pipe, disc)
		require.NoError(t, err)
		_, ok := result.([]instroutput.ClusterView)
		assert.True(t, ok, "cluster view must return []ClusterView, got %T", result)
	})

	t.Run("service view returns []ServiceView", func(t *testing.T) {
		mon := &fakeMonitoringSource{}
		pipe := &fakePipelineSource{}
		disc := &fakeDiscoverySource{
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "ns1", Name: "svc1"},
			},
		}

		result, err := run(context.Background(), "", "ns1", mon, pipe, disc)
		require.NoError(t, err)
		_, ok := result.([]instroutput.ServiceView)
		assert.True(t, ok, "service view must return []ServiceView, got %T", result)
	})
}
