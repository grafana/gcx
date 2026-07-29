//nolint:testpackage // white-box tests exercise unexported plan builders
package azure

import (
	"context"
	"errors"
	"testing"
)

func TestBuildPlan_AzureMonitorAlwaysAndADXPerCluster(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			if hasPrefix(args, []string{"kusto", "cluster", "list"}) {
				return []byte(`[{"name":"adx1","uri":"https://adx1.kusto.windows.net","resourceGroup":"rg1","state":"Running"}]`), nil, nil
			}
			return nil, nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	plan := BuildPlan(context.Background(), PlanInput{
		CLI:     cli,
		Stack:   "mystack",
		Account: Account{SubID: "sub-1", Name: "Sub One"},
	})

	if len(plan) != 2 {
		t.Fatalf("expected 2 suggestions (azure monitor + 1 adx), got %d", len(plan))
	}
	if plan[0].Spec.Token() != TokenAzureMonitor {
		t.Fatalf("first suggestion = %q, want azure-monitor", plan[0].Spec.Token())
	}
	if plan[1].Spec.Token() != TokenADX {
		t.Fatalf("second suggestion = %q, want adx", plan[1].Spec.Token())
	}
	if plan[1].Extra["clusterUrl"] != "https://adx1.kusto.windows.net" {
		t.Fatalf("adx clusterUrl = %q", plan[1].Extra["clusterUrl"])
	}
	if plan[1].Name != "gcx-mystack-adx-adx1" {
		t.Fatalf("adx name = %q", plan[1].Name)
	}
	if plan[1].Disabled {
		t.Fatalf("running ADX cluster should be selectable, got Disabled=true (%q)", plan[1].DisabledReason)
	}
}

func TestBuildPlan_StoppedADXClusterIsDisabled(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			if hasPrefix(args, []string{"kusto", "cluster", "list"}) {
				return []byte(`[{"name":"adx-stopped","uri":"https://adx-stopped.kusto.windows.net","resourceGroup":"rg1","state":"Stopped"}]`), nil, nil
			}
			return nil, nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	plan := BuildPlan(context.Background(), PlanInput{CLI: cli, Account: Account{SubID: "sub-1"}})
	if len(plan) != 2 {
		t.Fatalf("expected 2 suggestions (azure monitor + 1 adx), got %d", len(plan))
	}
	adx := plan[1]
	if adx.Spec.Token() != TokenADX {
		t.Fatalf("second suggestion = %q, want adx", adx.Spec.Token())
	}
	if !adx.Disabled {
		t.Fatal("stopped ADX cluster should be Disabled")
	}
	if adx.DisabledReason == "" {
		t.Fatal("disabled suggestion should carry a reason")
	}
}

func TestBuildPlan_ADXDiscoveryErrorIsTolerated(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			if hasPrefix(args, []string{"kusto", "cluster", "list"}) {
				return nil, []byte("extension not installed"), errors.New("exit 1")
			}
			return nil, nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	plan := BuildPlan(context.Background(), PlanInput{CLI: cli, Account: Account{SubID: "sub-1"}})
	if len(plan) != 1 {
		t.Fatalf("expected only azure monitor when ADX discovery fails, got %d", len(plan))
	}
}

func TestBuildPlan_PrivateNetworkFlaggedOnADXAndCosmos(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			switch {
			case hasPrefix(args, []string{"kusto", "cluster", "list"}):
				return []byte(`[{"name":"adx-priv","uri":"https://adx-priv.kusto.windows.net","resourceGroup":"rg1","state":"Running","publicNetworkAccess":"Disabled"}]`), nil, nil
			case hasPrefix(args, []string{"cosmosdb", "list"}):
				return []byte(`[{"name":"cosmos-pub","resourceGroup":"rg1","documentEndpoint":"https://cosmos-pub.documents.azure.com","publicNetworkAccess":"Enabled"}]`), nil, nil
			}
			return nil, nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	plan := BuildPlan(context.Background(), PlanInput{CLI: cli, Account: Account{SubID: "sub-1"}, IncludeCosmos: true})

	var adx, cosmos *Suggestion
	for i := range plan {
		switch plan[i].Spec.Token() {
		case TokenADX:
			adx = &plan[i]
		case TokenCosmos:
			cosmos = &plan[i]
		}
	}
	if adx == nil || cosmos == nil {
		t.Fatalf("expected both ADX and Cosmos suggestions, got %d total", len(plan))
	}
	if !adx.PrivateNetwork {
		t.Error("ADX cluster with publicNetworkAccess=Disabled should set PrivateNetwork")
	}
	if cosmos.PrivateNetwork {
		t.Error("Cosmos account with publicNetworkAccess=Enabled should not set PrivateNetwork")
	}
}

func TestBuildPlan_IncludeCosmos(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			if hasPrefix(args, []string{"cosmosdb", "list"}) {
				return []byte(`[{"name":"cosmos1","resourceGroup":"rg1","documentEndpoint":"https://cosmos1.documents.azure.com"}]`), nil, nil
			}
			return nil, nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	plan := BuildPlan(context.Background(), PlanInput{CLI: cli, Account: Account{SubID: "sub-1"}, IncludeCosmos: true})

	var found bool
	for _, s := range plan {
		if s.Spec.Token() == TokenCosmos {
			found = true
			if s.Extra["endpoint"] != "https://cosmos1.documents.azure.com" {
				t.Fatalf("cosmos endpoint = %q", s.Extra["endpoint"])
			}
		}
	}
	if !found {
		t.Fatal("expected a cosmos suggestion when IncludeCosmos is set")
	}
}
