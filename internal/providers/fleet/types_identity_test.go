package fleet_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/grafana/gcx/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &fleet.Pipeline{}
var _ adapter.ResourceIdentity = &fleet.Collector{}

func TestPipeline_ResourceIdentity(t *testing.T) {
	// GetResourceName returns slug-id composite when both Name and ID are set.
	p := &fleet.Pipeline{ID: "1", Name: "my pipeline"}
	if got := p.GetResourceName(); got != "my-pipeline-1" {
		t.Errorf("GetResourceName() = %q, want %q", got, "my-pipeline-1")
	}

	// GetResourceName falls back to bare ID when Name is empty.
	pNoName := &fleet.Pipeline{ID: "42"}
	if got := pNoName.GetResourceName(); got != "42" {
		t.Errorf("GetResourceName() (no name) = %q, want %q", got, "42")
	}

	// SetResourceName extracts the numeric ID from a slug-id composite.
	p.SetResourceName("my-pipeline-2")
	if p.ID != "2" {
		t.Errorf("SetResourceName (slug-id): ID = %q, want %q", p.ID, "2")
	}

	// SetResourceName stores the value directly when it's a plain numeric ID.
	p.SetResourceName("99")
	if p.ID != "99" {
		t.Errorf("SetResourceName (numeric): ID = %q, want %q", p.ID, "99")
	}
}

func TestCollector_ResourceIdentity(t *testing.T) {
	// GetResourceName returns slug-id composite when both Name and ID are set.
	c := &fleet.Collector{ID: "1", Name: "my collector"}
	if got := c.GetResourceName(); got != "my-collector-1" {
		t.Errorf("GetResourceName() = %q, want %q", got, "my-collector-1")
	}

	// GetResourceName falls back to bare ID when Name is empty.
	cNoName := &fleet.Collector{ID: "42"}
	if got := cNoName.GetResourceName(); got != "42" {
		t.Errorf("GetResourceName() (no name) = %q, want %q", got, "42")
	}

	// SetResourceName extracts the numeric ID from a slug-id composite.
	c.SetResourceName("my-collector-2")
	if c.ID != "2" {
		t.Errorf("SetResourceName (slug-id): ID = %q, want %q", c.ID, "2")
	}

	// SetResourceName stores the value directly when it's a plain numeric ID.
	c.SetResourceName("99")
	if c.ID != "99" {
		t.Errorf("SetResourceName (numeric): ID = %q, want %q", c.ID, "99")
	}

	// Non-numeric IDs (e.g. hostnames) round-trip through the resource name.
	cHost := &fleet.Collector{ID: "gke-dev-us-central-0-a9f49189-r6z4"}
	if got := cHost.GetResourceName(); got != "gke-dev-us-central-0-a9f49189-r6z4" {
		t.Errorf("GetResourceName() (non-numeric ID) = %q, want %q", got, "gke-dev-us-central-0-a9f49189-r6z4")
	}
	cHost.SetResourceName("gke-dev-us-central-0-a9f49189-r6z4")
	if cHost.ID != "gke-dev-us-central-0-a9f49189-r6z4" {
		t.Errorf("SetResourceName (non-numeric): ID = %q, want %q", cHost.ID, "gke-dev-us-central-0-a9f49189-r6z4")
	}
}
