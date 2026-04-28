package dashboards_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	_ "github.com/grafana/gcx/internal/providers/dashboards" // trigger self-registration
)

// TestDashboardsProviderRegistered verifies that importing the dashboards
// package registers a DashboardsProvider via init().
func TestDashboardsProviderRegistered(t *testing.T) {
	all := providers.All()
	for _, p := range all {
		if p.Name() == "dashboards" {
			return
		}
	}
	t.Fatal("expected dashboards provider to be registered via init(), but it was not found in providers.All()")
}

// TestDashboardsProviderMetadata verifies basic provider metadata.
func TestDashboardsProviderMetadata(t *testing.T) {
	var dashProvider providers.Provider
	for _, p := range providers.All() {
		if p.Name() == "dashboards" {
			dashProvider = p
			break
		}
	}

	if dashProvider == nil {
		t.Fatal("dashboards provider not registered")
	}

	if dashProvider.ShortDesc() == "" {
		t.Error("ShortDesc() must not be empty")
	}

	// Nil hooks are expected for a commands-only provider.
	if err := dashProvider.Validate(nil); err != nil {
		t.Errorf("Validate() = %v; want nil", err)
	}
	if keys := dashProvider.ConfigKeys(); keys != nil {
		t.Errorf("ConfigKeys() = %v; want nil", keys)
	}
	if regs := dashProvider.TypedRegistrations(); regs != nil {
		t.Errorf("TypedRegistrations() = %v; want nil", regs)
	}
}

// TestDashboardsProviderCommandSubtree verifies that the provider contributes
// the expected command subtree.
func TestDashboardsProviderCommandSubtree(t *testing.T) {
	var dashProvider providers.Provider
	for _, p := range providers.All() {
		if p.Name() == "dashboards" {
			dashProvider = p
			break
		}
	}
	if dashProvider == nil {
		t.Fatal("dashboards provider not registered")
	}

	cmds := dashProvider.Commands()
	if len(cmds) != 1 {
		t.Fatalf("Commands() returned %d commands; want 1 (the dashboards root)", len(cmds))
	}

	rootCmd := cmds[0]
	if rootCmd.Use != "dashboards" {
		t.Errorf("root command Use = %q; want \"dashboards\"", rootCmd.Use)
	}

	// Collect subcommand names.
	subNames := make(map[string]bool)
	for _, sub := range rootCmd.Commands() {
		subNames[sub.Name()] = true
	}

	wantSubs := []string{"list", "get", "create", "update", "delete", "search", "versions", "snapshot"}
	for _, want := range wantSubs {
		if !subNames[want] {
			t.Errorf("missing expected subcommand %q; got %v", want, subNames)
		}
	}
}
