package assistant_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	gcxerrors "github.com/grafana/gcx/internal/gcxerrors"
	assistant "github.com/grafana/gcx/internal/providers/assistant"
	"github.com/spf13/cobra"
)

// TestConventions_GroupCommandFlags verifies that the assistant group binds
// --config via providers.ConfigLoader.BindFlags but does NOT bind its own
// --context: --context is the root command's global persistent flag, threaded
// into context.Context and read back by the shared loader. A duplicate
// group-level --context binding would silently shadow the root flag (see
// cmd/gcx/root/command_test.go).
func TestConventions_GroupCommandFlags(t *testing.T) {
	cmd := assistant.Command()

	if cmd.PersistentFlags().Lookup("config") == nil {
		t.Fatal("expected assistant group command to have persistent --config flag (via providers.ConfigLoader.BindFlags)")
	}

	if cmd.PersistentFlags().Lookup("context") != nil {
		t.Fatal("assistant group must NOT bind its own --context flag; --context is the root command's global flag")
	}
}

// TestContextFlagReachesLoaderViaRootThreading proves the shared ConfigLoader
// resolves the active context from the root command's global --context flag
// (threaded into context.Context), regardless of flag position, now that the
// group no longer binds its own --context. The requireGrafanaCloud guard runs
// in the group's PersistentPreRunE off the loader's resolved context: a
// self-hosted target is blocked, a cloud target passes the guard and only then
// hits RunE flag validation.
func TestContextFlagReachesLoaderViaRootThreading(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(cfgPath, []byte(`current-context: cloud
contexts:
  cloud:
    grafana:
      server: https://mystack.grafana.net
  selfhosted:
    grafana:
      server: https://grafana.internal.example.com
`), 0o600); err != nil {
		t.Fatal(err)
	}

	newRoot := func() *cobra.Command {
		contextName := ""
		root := &cobra.Command{
			Use:           "gcx",
			SilenceUsage:  true,
			SilenceErrors: true,
			PersistentPreRun: func(cmd *cobra.Command, _ []string) {
				if contextName != "" {
					cmd.SetContext(config.ContextWithName(cmd.Context(), contextName))
				}
			},
		}
		root.PersistentFlags().StringVar(&contextName, "context", "", "context")
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.AddCommand(assistant.Command())
		return root
	}

	tests := []struct {
		name    string
		args    []string
		blocked bool // guard blocks (self-hosted) vs. passes to RunE validation (cloud)
	}{
		{
			name:    "context before subcommand selects self-hosted",
			args:    []string{"--context", "selfhosted", "assistant", "prompt", "hi", "--config", cfgPath, "--context-id", "a", "--continue"},
			blocked: true,
		},
		{
			name:    "context after group selects self-hosted",
			args:    []string{"assistant", "--context", "selfhosted", "prompt", "hi", "--config", cfgPath, "--context-id", "a", "--continue"},
			blocked: true,
		},
		{
			name:    "context selects cloud passes guard to RunE validation",
			args:    []string{"--context", "cloud", "assistant", "prompt", "hi", "--config", cfgPath, "--context-id", "a", "--continue"},
			blocked: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newRoot()
			root.SetArgs(tt.args)
			err := root.Execute()
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.blocked {
				var de gcxerrors.DetailedError
				if !errors.As(err, &de) {
					t.Fatalf("expected self-hosted guard DetailedError, got %T: %v", err, err)
				}
			} else if err.Error() != "cannot use both --context-id and --continue flags" {
				t.Fatalf("expected guard to pass and RunE validation to fire, got: %v", err)
			}
		})
	}
}

// TestConventions_AgentFlagNotNamedAgent verifies that the flag for specifying
// the target agent ID is NOT named "agent", since that conflicts with the root
// command's global --agent flag (bool for agent mode).
func TestConventions_AgentFlagNotNamedAgent(t *testing.T) {
	cmd := assistant.Command()

	var promptCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "prompt" {
			promptCmd = sub
			break
		}
	}
	if promptCmd == nil {
		t.Fatal("expected to find 'prompt' subcommand")
	}

	agentFlag := promptCmd.Flags().Lookup("agent")
	if agentFlag != nil {
		t.Fatal("prompt subcommand has --agent flag which conflicts with root command's global --agent flag; rename to --agent-id")
	}

	agentIDFlag := promptCmd.Flags().Lookup("agent-id")
	if agentIDFlag == nil {
		t.Fatal("expected prompt subcommand to have --agent-id flag for specifying the target agent")
	}
}

// TestConventions_ValidateExported verifies that opts validation works correctly
// by testing mutually exclusive flags through the command interface.
func TestConventions_ValidateExported(t *testing.T) {
	cmd := assistant.Command()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	// Write a minimal cloud config so PersistentPreRunE's cloud check passes,
	// letting us exercise the flag validation logic.
	cfgPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(cfgPath, []byte(`current-context: test
contexts:
  test:
    grafana:
      server: https://test.grafana.net
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd.SetArgs([]string{"prompt", "test", "--config", cfgPath, "--context-id", "abc", "--continue"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --context-id and --continue are set")
	}
	if err.Error() != "cannot use both --context-id and --continue flags" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestConventions_PromptAnnotations verifies that commands have agent annotations
// per project convention.
func TestConventions_PromptAnnotations(t *testing.T) {
	cmd := assistant.Command()

	var promptCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "prompt" {
			promptCmd = sub
			break
		}
	}
	if promptCmd == nil {
		t.Fatal("expected to find 'prompt' subcommand")
	}

	if _, ok := promptCmd.Annotations["agent.token_cost"]; !ok {
		t.Fatal("expected prompt command to have agent.token_cost annotation")
	}
}

// TestConventions_DashboardSubcommandExists verifies that the assistant group
// command exposes a 'dashboard' subcommand for discoverability of the
// dashboarding agent.
func TestConventions_DashboardSubcommandExists(t *testing.T) {
	cmd := assistant.Command()

	var dashCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "dashboard" {
			dashCmd = sub
			break
		}
	}
	if dashCmd == nil {
		t.Fatal("expected to find 'dashboard' subcommand under assistant")
	}
}

// TestConventions_DashboardSubcommandHasAnnotations verifies that the dashboard
// subcommand carries agent annotations matching the prompt subcommand.
func TestConventions_DashboardSubcommandHasAnnotations(t *testing.T) {
	cmd := assistant.Command()

	var dashCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "dashboard" {
			dashCmd = sub
			break
		}
	}
	if dashCmd == nil {
		t.Fatal("expected to find 'dashboard' subcommand")
	}

	if _, ok := dashCmd.Annotations["agent.token_cost"]; !ok {
		t.Fatal("expected dashboard command to have agent.token_cost annotation")
	}
}

// TestConventions_DashboardSubcommandNoAgentIDFlag verifies that the dashboard
// subcommand does NOT expose --agent-id, since its agent is fixed.
func TestConventions_DashboardSubcommandNoAgentIDFlag(t *testing.T) {
	cmd := assistant.Command()

	var dashCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "dashboard" {
			dashCmd = sub
			break
		}
	}
	if dashCmd == nil {
		t.Fatal("expected to find 'dashboard' subcommand")
	}

	if dashCmd.Flags().Lookup("agent-id") != nil {
		t.Fatal("dashboard subcommand should not expose --agent-id; the agent is fixed to grafana_dashboarding")
	}
}

func TestConventions_MCPServersCommandMounted(t *testing.T) {
	cmd := assistant.Command()

	var mcpCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "mcp-servers" {
			mcpCmd = sub
			break
		}
	}
	if mcpCmd == nil {
		t.Fatal("expected to find 'mcp-servers' subcommand")
	}

	for _, name := range []string{"list", "get", "create", "update", "delete"} {
		if sub, _, err := mcpCmd.Find([]string{name}); err != nil || sub.Name() != name {
			t.Fatalf("expected mcp-servers %s command to be mounted", name)
		}
	}
}

func TestRequireGrafanaCloud(t *testing.T) {
	tests := []struct {
		name    string
		ctx     *config.Context
		wantErr bool
	}{
		{
			name: "cloud instance via server URL",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana.net"},
			},
		},
		{
			name: "cloud instance via explicit stack slug",
			ctx: &config.Context{
				StackEntry: &config.StackConfig{Slug: "mystack"},
				Grafana:    &config.GrafanaConfig{Server: "https://custom.example.com"},
			},
		},
		{
			name: "grafana.com host (demokit-style)",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://emea.cloud.demokit.grafana.com"},
			},
		},
		{
			name: "grafana-dev.com host",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana-dev.com"},
			},
		},
		{
			name: "grafana-ops.com host",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana-ops.com"},
			},
		},
		{
			name: "grafana.stack-id non-zero with custom domain",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://grafana.example.com", StackID: 12345},
			},
		},
		{
			name: "bare grafana.com is not a subdomain",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://grafana.com"},
			},
			wantErr: true,
		},
		{
			name: "self-hosted instance",
			ctx: &config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://grafana.example.com"},
			},
			wantErr: true,
		},
		{
			name: "no grafana config skips check",
			ctx:  &config.Context{},
		},
		{
			name: "empty server skips check",
			ctx:  &config.Context{Grafana: &config.GrafanaConfig{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := assistant.RequireGrafanaCloud(tt.ctx)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var de gcxerrors.DetailedError
				if !errors.As(err, &de) {
					t.Fatalf("expected gcxerrors.DetailedError, got %T", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
