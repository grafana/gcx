package k6 //nolint:testpackage // Tests verify compile-time ResourceIdentity conformance on value types.

import (
	"testing"

	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var (
	_ adapter.ResourceIdentity = &Project{}
	_ adapter.ResourceIdentity = &LoadTest{}
	_ adapter.ResourceIdentity = &Schedule{}
	_ adapter.ResourceIdentity = &EnvVar{}
	_ adapter.ResourceIdentity = &LoadZone{}
)

func TestK6Types_ResourceIdentity(t *testing.T) {
	t.Run("Project int ID", func(t *testing.T) {
		p := &Project{ID: 42}
		if got := p.GetResourceName(); got != "42" {
			t.Errorf("GetResourceName() = %q, want %q", got, "42")
		}
		p.SetResourceName("99")
		if p.ID != 99 {
			t.Errorf("ID = %d, want 99", p.ID)
		}
		p.SetResourceName("not-a-number")
		if p.ID != 0 {
			t.Errorf("ID = %d after invalid name, want 0", p.ID)
		}
	})

	t.Run("LoadTest int ID", func(t *testing.T) {
		lt := &LoadTest{ID: 7}
		if got := lt.GetResourceName(); got != "7" {
			t.Errorf("GetResourceName() = %q, want %q", got, "7")
		}
	})

	t.Run("LoadZone string Name", func(t *testing.T) {
		lz := &LoadZone{Name: "us-east-1"}
		if got := lz.GetResourceName(); got != "us-east-1" {
			t.Errorf("GetResourceName() = %q, want %q", got, "us-east-1")
		}
		lz.SetResourceName("eu-west-1")
		if lz.Name != "eu-west-1" {
			t.Errorf("Name = %q, want %q", lz.Name, "eu-west-1")
		}
	})
}
