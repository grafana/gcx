package probes_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/synth/probes"
	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &probes.Probe{}

func TestProbe_ResourceIdentity(t *testing.T) {
	p := &probes.Probe{ID: 42}
	if got := p.GetResourceName(); got != "42" {
		t.Errorf("GetResourceName() = %q, want %q", got, "42")
	}
	p.SetResourceName("99")
	if p.ID != 99 {
		t.Errorf("ID = %d, want 99", p.ID)
	}
	// Parse error silently ignored.
	p.SetResourceName("not-a-number")
	if p.ID != 0 {
		t.Errorf("ID = %d after invalid SetResourceName, want 0", p.ID)
	}
}
