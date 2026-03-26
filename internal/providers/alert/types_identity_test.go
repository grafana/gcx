package alert

import (
	"testing"

	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &RuleStatus{}
var _ adapter.ResourceIdentity = &RuleGroup{}

func TestRuleStatus_ResourceIdentity(t *testing.T) {
	r := &RuleStatus{UID: "abc"}
	if got := r.GetResourceName(); got != "abc" {
		t.Errorf("GetResourceName() = %q, want %q", got, "abc")
	}
	r.SetResourceName("xyz")
	if r.UID != "xyz" {
		t.Errorf("UID = %q, want %q", r.UID, "xyz")
	}
}

func TestRuleGroup_ResourceIdentity(t *testing.T) {
	g := &RuleGroup{Name: "group-1"}
	if got := g.GetResourceName(); got != "group-1" {
		t.Errorf("GetResourceName() = %q, want %q", got, "group-1")
	}
	g.SetResourceName("group-2")
	if g.Name != "group-2" {
		t.Errorf("Name = %q, want %q", g.Name, "group-2")
	}
}
