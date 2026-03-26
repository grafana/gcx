package fleet_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/fleet"
	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &fleet.Pipeline{}
var _ adapter.ResourceIdentity = &fleet.Collector{}

func TestPipeline_ResourceIdentity(t *testing.T) {
	p := &fleet.Pipeline{ID: "pipe-1"}
	if got := p.GetResourceName(); got != "pipe-1" {
		t.Errorf("GetResourceName() = %q, want %q", got, "pipe-1")
	}
	p.SetResourceName("pipe-2")
	if p.ID != "pipe-2" {
		t.Errorf("ID = %q, want %q", p.ID, "pipe-2")
	}
}

func TestCollector_ResourceIdentity(t *testing.T) {
	c := &fleet.Collector{ID: "col-1"}
	if got := c.GetResourceName(); got != "col-1" {
		t.Errorf("GetResourceName() = %q, want %q", got, "col-1")
	}
	c.SetResourceName("col-2")
	if c.ID != "col-2" {
		t.Errorf("ID = %q, want %q", c.ID, "col-2")
	}
}
