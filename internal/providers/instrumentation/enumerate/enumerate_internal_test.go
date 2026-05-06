package enumerate

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

func TestK8sMonitoringClusterName_LiberalKeys(t *testing.T) {
	tests := []struct {
		name     string
		pipeline instrumentation.Pipeline
		wantName string
		wantOK   bool
	}{
		{
			name: "cluster key accepted",
			pipeline: instrumentation.Pipeline{
				Metadata: map[string]any{"type": "k8s_monitoring", "cluster": "prod-eu"},
			},
			wantName: "prod-eu",
			wantOK:   true,
		},
		{
			name: "cluster_name key accepted",
			pipeline: instrumentation.Pipeline{
				Metadata: map[string]any{"type": "k8s_monitoring", "cluster_name": "prod-eu"},
			},
			wantName: "prod-eu",
			wantOK:   true,
		},
		{
			name: "cluster_name takes precedence when both present",
			pipeline: instrumentation.Pipeline{
				Metadata: map[string]any{"type": "k8s_monitoring", "cluster": "old", "cluster_name": "new"},
			},
			wantName: "new",
			wantOK:   true,
		},
		{
			name: "wrong type rejected",
			pipeline: instrumentation.Pipeline{
				Metadata: map[string]any{"type": "beyla", "cluster_name": "prod-eu"},
			},
			wantName: "",
			wantOK:   false,
		},
		{
			name:     "nil metadata rejected",
			pipeline: instrumentation.Pipeline{Metadata: nil},
			wantName: "",
			wantOK:   false,
		},
		{
			name: "empty cluster name rejected",
			pipeline: instrumentation.Pipeline{
				Metadata: map[string]any{"type": "k8s_monitoring", "cluster_name": ""},
			},
			wantName: "",
			wantOK:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := k8sMonitoringClusterName(tc.pipeline)
			if got != tc.wantName || ok != tc.wantOK {
				t.Errorf("k8sMonitoringClusterName() = (%q, %v), want (%q, %v)", got, ok, tc.wantName, tc.wantOK)
			}
		})
	}
}
