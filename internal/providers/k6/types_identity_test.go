package k6_test

import (
	"testing"

	k6 "github.com/grafana/gcx/internal/providers/k6"
	"github.com/grafana/gcx/internal/resources/adapter"
)

var (
	_ adapter.ResourceIdentity = &k6.Project{}
	_ adapter.ResourceIdentity = &k6.LoadTest{}
	_ adapter.ResourceIdentity = &k6.Schedule{}
	_ adapter.ResourceIdentity = &k6.EnvVar{}
	_ adapter.ResourceIdentity = &k6.LoadZone{}
)

func TestK6Types_ResourceIdentity(t *testing.T) {
	t.Run("Project int ID", func(t *testing.T) {
		p := &k6.Project{ID: 42}
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
		lt := &k6.LoadTest{ID: 7}
		if got := lt.GetResourceName(); got != "7" {
			t.Errorf("GetResourceName() = %q, want %q", got, "7")
		}
	})

	t.Run("LoadZone string Name", func(t *testing.T) {
		lz := &k6.LoadZone{Name: "us-east-1"}
		if got := lz.GetResourceName(); got != "us-east-1" {
			t.Errorf("GetResourceName() = %q, want %q", got, "us-east-1")
		}
		lz.SetResourceName("eu-west-1")
		if lz.Name != "eu-west-1" {
			t.Errorf("Name = %q, want %q", lz.Name, "eu-west-1")
		}
	})
}
