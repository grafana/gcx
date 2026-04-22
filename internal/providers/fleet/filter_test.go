package fleet_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/stretchr/testify/assert"
)

func TestFilterCollectorsByCluster(t *testing.T) {
	tests := []struct {
		name       string
		collectors []fleet.Collector
		cluster    string
		wantIDs    []string
	}{
		{
			name:       "empty input",
			collectors: nil,
			cluster:    "prod-east",
			wantIDs:    nil,
		},
		{
			name: "remote_attributes cluster key match",
			collectors: []fleet.Collector{
				{ID: "a", RemoteAttributes: map[string]string{"cluster": "prod-east"}},
				{ID: "b", RemoteAttributes: map[string]string{"cluster": "prod-west"}},
			},
			cluster: "prod-east",
			wantIDs: []string{"a"},
		},
		{
			name: "local_attributes cluster key match",
			collectors: []fleet.Collector{
				{ID: "a", LocalAttributes: map[string]string{"cluster": "prod-east"}},
				{ID: "b", LocalAttributes: map[string]string{"cluster": "prod-west"}},
			},
			cluster: "prod-east",
			wantIDs: []string{"a"},
		},
		{
			name: "alternate key k8s_cluster",
			collectors: []fleet.Collector{
				{ID: "a", RemoteAttributes: map[string]string{"k8s_cluster": "prod-east"}},
				{ID: "b", RemoteAttributes: map[string]string{"k8s_cluster": "prod-west"}},
			},
			cluster: "prod-east",
			wantIDs: []string{"a"},
		},
		{
			name: "alternate key cluster_name",
			collectors: []fleet.Collector{
				{ID: "a", RemoteAttributes: map[string]string{"cluster_name": "prod-east"}},
			},
			cluster: "prod-east",
			wantIDs: []string{"a"},
		},
		{
			name: "no matching key returns empty (not error)",
			collectors: []fleet.Collector{
				{ID: "a", RemoteAttributes: map[string]string{"region": "us-east"}},
				{ID: "b"},
			},
			cluster: "prod-east",
			wantIDs: nil,
		},
		{
			name: "case-sensitive match",
			collectors: []fleet.Collector{
				{ID: "a", RemoteAttributes: map[string]string{"cluster": "Prod-East"}},
			},
			cluster: "prod-east",
			wantIDs: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fleet.FilterCollectorsByCluster(tc.collectors, tc.cluster)
			var gotIDs []string
			for _, c := range got {
				gotIDs = append(gotIDs, c.ID)
			}
			assert.Equal(t, tc.wantIDs, gotIDs)
		})
	}
}
