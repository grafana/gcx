package adapter_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
)

func TestProvider_CommandsWithoutFactory(t *testing.T) {
	p := adapter.NewProvider("test", "Test provider", nil)
	if commands := p.Commands(); commands != nil {
		t.Fatalf("Commands() = %v, want nil", commands)
	}
}
