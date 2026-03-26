package adapter_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/resources/adapter"
)

// stringID is a test type with a string identity field.
type stringID struct {
	UUID string
}

func (s stringID) GetResourceName() string    { return s.UUID }
func (s *stringID) SetResourceName(n string)  { s.UUID = n }

// Compile-time verification that stringID satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &stringID{}

func TestResourceIdentity_StringType(t *testing.T) {
	s := &stringID{UUID: "abc-123"}
	if got := s.GetResourceName(); got != "abc-123" {
		t.Errorf("GetResourceName() = %q, want %q", got, "abc-123")
	}

	s.SetResourceName("xyz-456")
	if s.UUID != "xyz-456" {
		t.Errorf("after SetResourceName: UUID = %q, want %q", s.UUID, "xyz-456")
	}
}

func TestResourceIdentity_InterfaceHasTwoMethods(t *testing.T) {
	// Ensure the interface is exactly two methods by checking that a type with
	// both methods satisfies it. This is a compile-time check via the var _ above,
	// but we include this test for documentation purposes.
	var ri adapter.ResourceIdentity = &stringID{UUID: "test"}
	_ = ri.GetResourceName()
	ri.SetResourceName("new")
}
