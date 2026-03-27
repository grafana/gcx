package kg_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertions for ResourceIdentity compliance.
var (
	_ adapter.ResourceIdentity = &kg.Rule{}
	_ adapter.ResourceIdentity = &kg.DatasetItem{}
	_ adapter.ResourceIdentity = &kg.Vendor{}
	_ adapter.ResourceIdentity = &kg.GraphEntity{}
	_ adapter.ResourceIdentity = &kg.EntityType{}
	_ adapter.ResourceIdentity = &kg.Scope{}
)

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

func TestAllTypes_ResourceIdentity(t *testing.T) {
	tests := []struct {
		name         string
		obj          adapter.ResourceIdentity
		wantName     string
		wantAfterSet string // expected GetResourceName after SetResourceName("new-name")
	}{
		{"DatasetItem", &kg.DatasetItem{Name: "kubernetes"}, "kubernetes", "new-name"},
		{"Vendor", &kg.Vendor{Name: "nginx"}, "nginx", "new-name"},
		// GraphEntity has composite identity: Type--Name. SetResourceName only sets Name.
		{"GraphEntity", &kg.GraphEntity{Type: "Service", Name: "frontend"}, "Service--frontend", "Service--new-name"},
		{"EntityType", &kg.EntityType{Name: "Service", Count: 42}, "Service", "new-name"},
		{"Scope", &kg.Scope{Name: "env", Values: []string{"prod"}}, "env", "new-name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.obj.GetResourceName(); got != tt.wantName {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.wantName)
			}
			tt.obj.SetResourceName("new-name")
			if got := tt.obj.GetResourceName(); got != tt.wantAfterSet {
				t.Errorf("after SetResourceName(), GetResourceName() = %q, want %q", got, tt.wantAfterSet)
			}
		})
	}
}
