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

	// SetResourceName extracts the numeric ID from a legacy slug-id composite.
	legacyComposite := &fleet.Collector{}
	legacyComposite.SetResourceName("my-collector-2")
	if legacyComposite.ID != "2" {
		t.Errorf("SetResourceName (slug-id): ID = %q, want %q", legacyComposite.ID, "2")
	}

	// SetResourceName stores the value directly when it's a plain numeric ID.
	legacyNumeric := &fleet.Collector{}
	legacyNumeric.SetResourceName("99")
	if legacyNumeric.ID != "99" {
		t.Errorf("SetResourceName (numeric): ID = %q, want %q", legacyNumeric.ID, "99")
	}

	// SetResourceName preserves a canonical string ID from spec.id.
	stringID := &fleet.Collector{ID: "collector-prod-eu-a"}
	stringID.SetResourceName("my-collector-123")
	if stringID.ID != "collector-prod-eu-a" {
		t.Errorf("SetResourceName (canonical ID): ID = %q, want %q", stringID.ID, "collector-prod-eu-a")
	}

	// A legacy nonnumeric metadata name cannot recover a canonical ID.
	legacyStringID := &fleet.Collector{}
	legacyStringID.SetResourceName("my-collector")
	if legacyStringID.ID != "" {
		t.Errorf("SetResourceName (legacy string name): ID = %q, want empty", legacyStringID.ID)
	}
}
