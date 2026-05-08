package enumerate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/enumerate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMonitoringClient implements enumerate.MonitoringClient using a fixed
// slice. An error field lets tests exercise error paths.
type fakeMonitoringClient struct {
	clusters []instrumentation.ClusterObservedState
	err      error
}

func (f *fakeMonitoringClient) RunK8sMonitoring(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
	return f.clusters, f.err
}

// fakePipelineClient implements enumerate.PipelineClient using a fixed slice.
type fakePipelineClient struct {
	pipelines []instrumentation.Pipeline
	err       error
}

func (f *fakePipelineClient) ListPipelines(_ context.Context) ([]instrumentation.Pipeline, error) {
	return f.pipelines, f.err
}

// k8sPipeline builds a K8s monitoring pipeline with the given cluster name.
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

// otherPipeline builds a pipeline that is NOT a K8s monitoring pipeline.
func otherPipeline(id string) instrumentation.Pipeline {
	return instrumentation.Pipeline{
		ID:   id,
		Name: "beyla-survey",
		Metadata: map[string]any{
			"type": "beyla_survey",
		},
	}
}

func TestEnumerate(t *testing.T) {
	tests := []struct {
		name string

		monClusters []instrumentation.ClusterObservedState
		pipelines   []instrumentation.Pipeline

		monErr  error
		pipeErr error

		wantErr    bool
		wantNames  []string // expected cluster names, in sorted order
		wantChecks func(t *testing.T, got []enumerate.ObservedCluster)
	}{
		{
			name:        "both empty returns empty result",
			monClusters: nil,
			pipelines:   nil,
			wantNames:   nil,
		},
		{
			name: "only monitoring populated returns those clusters with MonitoringSource",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "cluster-b", InstrumentationStatus: instrumentation.StatusInstrumented},
				{Name: "cluster-a", InstrumentationStatus: instrumentation.StatusPendingInstrumentation},
			},
			pipelines: nil,
			wantNames: []string{"cluster-a", "cluster-b"},
			wantChecks: func(t *testing.T, got []enumerate.ObservedCluster) {
				t.Helper()
				for _, oc := range got {
					assert.Equal(t, enumerate.MonitoringSource, oc.Source, "cluster %q should have MonitoringSource", oc.Name)
					assert.NotNil(t, oc.State, "cluster %q should have non-nil State", oc.Name)
				}
				assert.Equal(t, "cluster-a", got[0].Name)
				assert.Equal(t, instrumentation.StatusPendingInstrumentation, got[0].Status)
				assert.Equal(t, "cluster-b", got[1].Name)
				assert.Equal(t, instrumentation.StatusInstrumented, got[1].Status)
			},
		},
		{
			name:        "only pipelines (pre-Alloy) returns cluster with StatusPendingInstrumentation",
			monClusters: nil,
			pipelines:   []instrumentation.Pipeline{k8sPipeline("p1", "new-cluster")},
			wantNames:   []string{"new-cluster"},
			wantChecks: func(t *testing.T, got []enumerate.ObservedCluster) {
				t.Helper()
				require.Len(t, got, 1)
				assert.Equal(t, enumerate.PipelineSource, got[0].Source)
				assert.Equal(t, instrumentation.StatusPendingInstrumentation, got[0].Status)
				assert.Nil(t, got[0].State, "pre-Alloy cluster should have nil State")
			},
		},
		{
			name: "both populated disjoint returns merged and sorted result",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "zebra-cluster", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipelines: []instrumentation.Pipeline{
				k8sPipeline("p1", "alpha-cluster"),
			},
			wantNames: []string{"alpha-cluster", "zebra-cluster"},
			wantChecks: func(t *testing.T, got []enumerate.ObservedCluster) {
				t.Helper()
				require.Len(t, got, 2)
				// alpha-cluster: from pipeline only
				assert.Equal(t, "alpha-cluster", got[0].Name)
				assert.Equal(t, enumerate.PipelineSource, got[0].Source)
				assert.Equal(t, instrumentation.StatusPendingInstrumentation, got[0].Status)
				assert.Nil(t, got[0].State)
				// zebra-cluster: from monitoring only
				assert.Equal(t, "zebra-cluster", got[1].Name)
				assert.Equal(t, enumerate.MonitoringSource, got[1].Source)
				assert.Equal(t, instrumentation.StatusInstrumented, got[1].Status)
				assert.NotNil(t, got[1].State)
			},
		},
		{
			name: "both populated with overlap uses monitoring-side status and BothSources",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "shared-cluster", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipelines: []instrumentation.Pipeline{
				k8sPipeline("p1", "shared-cluster"),
			},
			wantNames: []string{"shared-cluster"},
			wantChecks: func(t *testing.T, got []enumerate.ObservedCluster) {
				t.Helper()
				require.Len(t, got, 1)
				assert.Equal(t, enumerate.BothSources, got[0].Source)
				assert.Equal(t, instrumentation.StatusInstrumented, got[0].Status,
					"monitoring-side status must win on overlap")
				assert.NotNil(t, got[0].State)
			},
		},
		{
			name:        "non-K8s-monitoring pipelines are filtered out",
			monClusters: nil,
			pipelines: []instrumentation.Pipeline{
				otherPipeline("p1"),
				{
					ID:   "p2",
					Name: "no-metadata-pipeline",
					// no Metadata
				},
				{
					ID:   "p3",
					Name: "wrong-type",
					Metadata: map[string]any{
						"type":    "app_monitoring",
						"cluster": "some-cluster",
					},
				},
				{
					ID:   "p4",
					Name: "missing-cluster-key",
					Metadata: map[string]any{
						"type": "k8s_monitoring",
						// no "cluster" key
					},
				},
				{
					ID:   "p5",
					Name: "empty-cluster-name",
					Metadata: map[string]any{
						"type":    "k8s_monitoring",
						"cluster": "",
					},
				},
			},
			wantNames: nil, // all pipelines filtered
		},
		{
			name:        "monitoring error is propagated",
			monClusters: nil,
			pipelines:   nil,
			monErr:      errors.New("monitoring RPC failed"),
			wantErr:     true,
		},
		{
			name:        "pipeline error is propagated",
			monClusters: nil,
			pipelines:   nil,
			pipeErr:     errors.New("pipeline RPC failed"),
			wantErr:     true,
		},
		{
			name: "multiple monitoring clusters are sorted alphabetically",
			monClusters: []instrumentation.ClusterObservedState{
				{Name: "charlie", InstrumentationStatus: instrumentation.StatusInstrumented},
				{Name: "alice", InstrumentationStatus: instrumentation.StatusInstrumented},
				{Name: "bob", InstrumentationStatus: instrumentation.StatusInstrumented},
			},
			pipelines: nil,
			wantNames: []string{"alice", "bob", "charlie"},
		},
		{
			name:        "multiple pipeline clusters are sorted alphabetically",
			monClusters: nil,
			pipelines: []instrumentation.Pipeline{
				k8sPipeline("p3", "charlie"),
				k8sPipeline("p1", "alice"),
				k8sPipeline("p2", "bob"),
			},
			wantNames: []string{"alice", "bob", "charlie"},
		},
		{
			name: "cluster present in monitoring has State pointer to correct entry",
			monClusters: []instrumentation.ClusterObservedState{
				{
					Name:                        "my-cluster",
					InstrumentationStatus:       instrumentation.StatusInstrumented,
					InstrumentationErrorMessage: "",
					Nodes:                       3,
					Workloads:                   10,
					Pods:                        20,
				},
			},
			pipelines: nil,
			wantNames: []string{"my-cluster"},
			wantChecks: func(t *testing.T, got []enumerate.ObservedCluster) {
				t.Helper()
				require.Len(t, got, 1)
				require.NotNil(t, got[0].State)
				assert.Equal(t, "my-cluster", got[0].State.Name)
				assert.Equal(t, 3, got[0].State.Nodes)
				assert.Equal(t, 10, got[0].State.Workloads)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mon := &fakeMonitoringClient{clusters: tt.monClusters, err: tt.monErr}
			pipe := &fakePipelineClient{pipelines: tt.pipelines, err: tt.pipeErr}

			got, err := enumerate.Enumerate(context.Background(), mon, pipe)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Check names (sorted). Use append so the result stays nil when
			// got is empty — matching wantNames: nil in table rows.
			var gotNames []string
			for _, oc := range got {
				gotNames = append(gotNames, oc.Name)
			}
			assert.Equal(t, tt.wantNames, gotNames)

			// Run per-test additional assertions.
			if tt.wantChecks != nil {
				tt.wantChecks(t, got)
			}
		})
	}
}
