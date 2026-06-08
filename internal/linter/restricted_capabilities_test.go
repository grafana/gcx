package linter_test

import (
	"testing"

	"github.com/grafana/gcx/internal/linter"
	"github.com/open-policy-agent/opa/v1/ast"
)

func TestRestrictedCapabilities_AllowNetIsEmpty(t *testing.T) {
	caps := linter.RestrictedCapabilities()
	if len(caps.AllowNet) != 0 {
		t.Errorf("expected AllowNet == []string{}, got %v", caps.AllowNet)
	}
}

func TestRestrictedCapabilities_PreservesDefaults(t *testing.T) {
	defaults := ast.CapabilitiesForThisVersion()
	caps := linter.RestrictedCapabilities()

	if len(caps.FutureKeywords) != len(defaults.FutureKeywords) {
		t.Errorf("FutureKeywords: got %d, want %d", len(caps.FutureKeywords), len(defaults.FutureKeywords))
	}
	if len(caps.Features) != len(defaults.Features) {
		t.Errorf("Features: got %d, want %d", len(caps.Features), len(defaults.Features))
	}
	if len(caps.WasmABIVersions) != len(defaults.WasmABIVersions) {
		t.Errorf("WasmABIVersions: got %d, want %d", len(caps.WasmABIVersions), len(defaults.WasmABIVersions))
	}
}

func TestRestrictedCapabilities_StripsNetworkBuiltins(t *testing.T) {
	caps := linter.RestrictedCapabilities()
	for _, b := range caps.Builtins {
		switch {
		case b.Name == "http.send":
			t.Errorf("http.send must not be in restricted capabilities")
		case b.Name == "opa.runtime":
			t.Errorf("opa.runtime must not be in restricted capabilities")
		case len(b.Name) > 4 && b.Name[:4] == "net.":
			t.Errorf("net.* builtin %q must not be in restricted capabilities", b.Name)
		}
	}
}
