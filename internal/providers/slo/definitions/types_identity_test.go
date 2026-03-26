package definitions_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/slo/definitions"
	"github.com/grafana/grafanactl/internal/resources/adapter"
)

var _ adapter.ResourceIdentity = &definitions.Slo{}

func TestSlo_ResourceIdentity(t *testing.T) {
	s := &definitions.Slo{UUID: "abc-123"}
	if got := s.GetResourceName(); got != "abc-123" {
		t.Errorf("GetResourceName() = %q, want %q", got, "abc-123")
	}
	s.SetResourceName("xyz-456")
	if s.UUID != "xyz-456" {
		t.Errorf("UUID = %q, want %q", s.UUID, "xyz-456")
	}
}
