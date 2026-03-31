package adaptive_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	_ "github.com/grafana/gcx/internal/providers/adaptive"
)

func TestProviderRegistered(t *testing.T) {
	var found bool
	for _, p := range providers.All() {
		if p.Name() == "adaptive" {
			found = true
			if got := p.ShortDesc(); got != "Manage Grafana Cloud Adaptive Telemetry." {
				t.Errorf("ShortDesc() = %q, want %q", got, "Manage Grafana Cloud Adaptive Telemetry.")
			}
			if err := p.Validate(nil); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
			break
		}
	}
	if !found {
		t.Error("adaptive provider not found in providers.All()")
	}
}

func TestConfigKeys(t *testing.T) {
	var p providers.Provider
	for _, pp := range providers.All() {
		if pp.Name() == "adaptive" {
			p = pp
			break
		}
	}
	if p == nil {
		t.Fatal("adaptive provider not found")
	}

	keys := p.ConfigKeys()

	want := map[string]bool{
		"metrics-tenant-id":  false,
		"metrics-tenant-url": false,
		"logs-tenant-id":     false,
		"logs-tenant-url":    false,
		"traces-tenant-id":   false,
		"traces-tenant-url":  false,
	}

	if len(keys) != len(want) {
		t.Fatalf("ConfigKeys() returned %d keys, want %d", len(keys), len(want))
	}

	for _, k := range keys {
		expectedSecret, ok := want[k.Name]
		if !ok {
			t.Errorf("unexpected ConfigKey %q", k.Name)
			continue
		}
		if k.Secret != expectedSecret {
			t.Errorf("ConfigKey %q: Secret = %v, want %v", k.Name, k.Secret, expectedSecret)
		}
	}
}

func TestTypedRegistrations(t *testing.T) {
	var p providers.Provider
	for _, pp := range providers.All() {
		if pp.Name() == "adaptive" {
			p = pp
			break
		}
	}
	if p == nil {
		t.Fatal("adaptive provider not found")
	}

	regs := p.TypedRegistrations()
	if len(regs) != 2 {
		t.Fatalf("TypedRegistrations() returned %d, want 2", len(regs))
	}

	// Verify Exemption registration.
	if regs[0].GVK.Kind != "Exemption" {
		t.Errorf("registration[0] Kind = %q, want %q", regs[0].GVK.Kind, "Exemption")
	}
	if regs[0].Schema == nil {
		t.Error("registration[0] Schema is nil")
	}
	if regs[0].Example == nil {
		t.Error("registration[0] Example is nil")
	}

	// Verify Policy registration.
	if regs[1].GVK.Kind != "Policy" {
		t.Errorf("registration[1] Kind = %q, want %q", regs[1].GVK.Kind, "Policy")
	}
	if regs[1].Schema == nil {
		t.Error("registration[1] Schema is nil")
	}
	if regs[1].Example == nil {
		t.Error("registration[1] Example is nil")
	}
}

func TestCommandTree(t *testing.T) {
	var p providers.Provider
	for _, pp := range providers.All() {
		if pp.Name() == "adaptive" {
			p = pp
			break
		}
	}
	if p == nil {
		t.Fatal("adaptive provider not found")
	}

	cmds := p.Commands()
	if len(cmds) != 1 {
		t.Fatalf("Commands() returned %d commands, want 1", len(cmds))
	}

	adaptive := cmds[0]
	if adaptive.Use != "adaptive" {
		t.Errorf("root command Use = %q, want %q", adaptive.Use, "adaptive")
	}

	// Check subcommands: metrics, logs, traces
	subs := adaptive.Commands()
	subNames := make(map[string]bool)
	for _, s := range subs {
		subNames[s.Use] = true
	}

	for _, want := range []string{"metrics", "logs", "traces"} {
		if !subNames[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}
