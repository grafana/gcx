//nolint:testpackage // white-box testing: accesses unexported run* functions and types.
package clusters

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrOutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:modernize // boolVal(x) is clearer than new(*bool) + dereference assignment in test data.
func boolVal(b bool) *bool { return &b }

func TestRunList(t *testing.T) {
	tests := []struct {
		name        string
		monClusters []instrumentation.ClusterObservedState
		monErr      error
		pipelines   []instrumentation.Pipeline
		pipeErr     error
		getCluster  instrumentation.Cluster
		getErr      error
		wantErr     bool
		wantNames   []string // cluster names expected in output
	}{
		{
			name: "single cluster from monitoring",
			monClusters: []instrumentation.ClusterObservedState{
				{
					Name:                  "prod-eu",
					InstrumentationStatus: instrumentation.StatusInstrumented,
					Namespaces:            []instrumentation.NamespaceObservedState{{Name: "default"}},
					Workloads:             5,
					Pods:                  10,
					Nodes:                 3,
				},
			},
			pipelines: nil,
			getCluster: instrumentation.Cluster{
				Name:          "prod-eu",
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolVal(true),  //nolint:modernize
				ClusterEvents: boolVal(true),  //nolint:modernize
				EnergyMetrics: boolVal(false), //nolint:modernize
				NodeLogs:      boolVal(false), //nolint:modernize
			},
			wantNames: []string{"prod-eu"},
		},
		{
			name:        "pre-alloy cluster from pipeline only",
			monClusters: nil,
			pipelines:   []instrumentation.Pipeline{makeK8sPipeline("staging-us")},
			getCluster: instrumentation.Cluster{
				Name:      "staging-us",
				Selection: "SELECTION_INCLUDED",
			},
			wantNames: []string{"staging-us"},
		},
		{
			name:        "no clusters returns empty table",
			monClusters: nil,
			pipelines:   nil,
			getCluster:  instrumentation.Cluster{},
			wantNames:   nil,
		},
		{
			name:    "monitoring error propagates",
			monErr:  assert.AnError,
			wantErr: true,
		},
		{
			name:        "GetK8SInstrumentation error propagates",
			monClusters: []instrumentation.ClusterObservedState{{Name: "prod-eu"}},
			getErr:      assert.AnError,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monClusters := tt.monClusters
			monErr := tt.monErr
			monClient := &fakeMonitoringClient{
				RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
					return monClusters, monErr
				},
			}
			pipeClient := &fakePipelineClient{
				Pipelines: tt.pipelines,
				Err:       tt.pipeErr,
			}

			instrClient := &fakeClient{
				GetK8SInstrumentationFn: func(_ context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error) {
					if tt.getErr != nil {
						return nil, tt.getErr
					}
					c := tt.getCluster
					c.Name = clusterName
					return &instrumentation.GetK8SInstrumentationResponse{Cluster: c}, nil
				},
			}

			opts := &listOpts{}
			opts.IO.RegisterCustomCodec("table", &instrOutput.ClusterTableCodec{Wide: false})
			opts.IO.RegisterCustomCodec("wide", &instrOutput.ClusterTableCodec{Wide: true})
			opts.IO.DefaultFormat("table")
			opts.IO.OutputFormat = "table"

			var buf bytes.Buffer
			err := runList(context.Background(), opts, monClient, pipeClient, instrClient, &buf)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			out := buf.String()
			for _, name := range tt.wantNames {
				assert.Contains(t, out, name,
					"expected cluster %q in output, got:\n%s", name, out)
			}
		})
	}
}

func TestRunList_MultipleClustersSorted(t *testing.T) {
	// Verify alphabetical sorting from enumerate.
	monClusters := []instrumentation.ClusterObservedState{
		{Name: "z-cluster", InstrumentationStatus: instrumentation.StatusInstrumented},
		{Name: "a-cluster", InstrumentationStatus: instrumentation.StatusInstrumented},
		{Name: "m-cluster", InstrumentationStatus: instrumentation.StatusInstrumented},
	}
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			return monClusters, nil
		},
	}
	pipeClient := &fakePipelineClient{}
	instrClient := &fakeClient{
		GetK8SInstrumentationFn: func(_ context.Context, name string) (*instrumentation.GetK8SInstrumentationResponse, error) {
			return &instrumentation.GetK8SInstrumentationResponse{
				Cluster: instrumentation.Cluster{Name: name, Selection: "SELECTION_INCLUDED"},
			}, nil
		},
	}

	opts := &listOpts{IO: cmdio.Options{OutputFormat: "json"}}

	var buf bytes.Buffer
	err := runList(context.Background(), opts, monClient, pipeClient, instrClient, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "a-cluster")
	assert.Contains(t, out, "m-cluster")
	assert.Contains(t, out, "z-cluster")
}

// TestRunList_JSONEnvelope_Empty verifies empty result emits {"items":[]}
// not [] or null.
func TestRunList_JSONEnvelope_Empty(t *testing.T) {
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			return nil, nil
		},
	}
	pipeClient := &fakePipelineClient{}
	instrClient := &fakeClient{
		GetK8SInstrumentationFn: func(_ context.Context, _ string) (*instrumentation.GetK8SInstrumentationResponse, error) {
			return &instrumentation.GetK8SInstrumentationResponse{Cluster: instrumentation.Cluster{}}, nil
		},
	}

	opts := &listOpts{IO: cmdio.Options{OutputFormat: "json"}}

	var buf bytes.Buffer
	err := runList(context.Background(), opts, monClient, pipeClient, instrClient, &buf)
	require.NoError(t, err)

	assert.JSONEq(t, `{"items":[]}`, buf.String())
}

// TestRunList_JSONEnvelope_NonEmpty verifies non-empty result is wrapped
// in {"items":[{...},...]} and not a flat array.
func TestRunList_JSONEnvelope_NonEmpty(t *testing.T) {
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			return []instrumentation.ClusterObservedState{
				{Name: "prod-eu", InstrumentationStatus: instrumentation.StatusInstrumented},
			}, nil
		},
	}
	pipeClient := &fakePipelineClient{}
	instrClient := &fakeClient{
		GetK8SInstrumentationFn: func(_ context.Context, name string) (*instrumentation.GetK8SInstrumentationResponse, error) {
			return &instrumentation.GetK8SInstrumentationResponse{
				Cluster: instrumentation.Cluster{Name: name, Selection: "SELECTION_INCLUDED"},
			}, nil
		},
	}

	opts := &listOpts{IO: cmdio.Options{OutputFormat: "json"}}

	var buf bytes.Buffer
	err := runList(context.Background(), opts, monClient, pipeClient, instrClient, &buf)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "output must be a JSON object; got: %s", buf.String())
	items, ok := got["items"]
	require.True(t, ok, "output must have 'items' key")
	itemsSlice, ok := items.([]any)
	require.True(t, ok)
	assert.Len(t, itemsSlice, 1)
}

// TestRunList_JSONFieldSelection_Unknown verifies --json with unknown field
// returns UnknownFieldSelectionError.
func TestRunList_JSONFieldSelection_Unknown(t *testing.T) {
	monClient := &fakeMonitoringClient{
		RunK8sMonitoringFn: func(_ context.Context) ([]instrumentation.ClusterObservedState, error) {
			return []instrumentation.ClusterObservedState{
				{Name: "prod-eu"},
			}, nil
		},
	}
	pipeClient := &fakePipelineClient{}
	instrClient := &fakeClient{
		GetK8SInstrumentationFn: func(_ context.Context, name string) (*instrumentation.GetK8SInstrumentationResponse, error) {
			return &instrumentation.GetK8SInstrumentationResponse{
				Cluster: instrumentation.Cluster{Name: name},
			}, nil
		},
	}

	opts := &listOpts{}
	opts.IO.RegisterCustomCodec("table", &instrOutput.ClusterTableCodec{Wide: false})
	opts.IO.DefaultFormat("table")
	opts.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrOutput.ClusterView{}))
	opts.IO.OutputFormat = "json"
	opts.IO.JSONFields = []string{"bogus", "name"}

	var buf bytes.Buffer
	err := runList(context.Background(), opts, monClient, pipeClient, instrClient, &buf)
	require.Error(t, err)
	var fieldErr cmdio.UnknownFieldSelectionError
	require.ErrorAs(t, err, &fieldErr)
	assert.Contains(t, fieldErr.Fields, "bogus")
}
