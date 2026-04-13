package irm_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/grafana/gcx/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &irm.Incident{}

func TestIncident_ResourceIdentity(t *testing.T) {
	i := &irm.Incident{IncidentID: "inc-42"}
	if got := i.GetResourceName(); got != "inc-42" {
		t.Errorf("GetResourceName() = %q, want %q", got, "inc-42")
	}
	i.SetResourceName("inc-99")
	if i.IncidentID != "inc-99" {
		t.Errorf("IncidentID = %q, want %q", i.IncidentID, "inc-99")
	}
}
