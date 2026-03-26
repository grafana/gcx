package checks //nolint:testpackage // Tests unexported checkResource type.

import (
	"testing"

	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &checkResource{}

func TestCheckResource_ResourceIdentity(t *testing.T) {
	cr := &checkResource{name: "web-check-1001"}
	if got := cr.GetResourceName(); got != "web-check-1001" {
		t.Errorf("GetResourceName() = %q, want %q", got, "web-check-1001")
	}
	cr.SetResourceName("new-name-42")
	if cr.name != "new-name-42" {
		t.Errorf("name = %q, want %q", cr.name, "new-name-42")
	}
}
