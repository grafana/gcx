package kg_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/kg"
	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &kg.Rule{}

func TestRule_ResourceIdentity(t *testing.T) {
	r := &kg.Rule{Name: "my-rule"}
	if got := r.GetResourceName(); got != "my-rule" {
		t.Errorf("GetResourceName() = %q, want %q", got, "my-rule")
	}
	r.SetResourceName("new-rule")
	if r.Name != "new-rule" {
		t.Errorf("Name = %q, want %q", r.Name, "new-rule")
	}
}
