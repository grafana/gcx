package assistant_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/assistant"
	"github.com/spf13/cobra"
)

// TestConventions_GroupCommandHasConfigFlags verifies that the assistant group command
// binds persistent config flags (--config, --context) via cmdconfig.Options.BindFlags,
// matching the pattern used by other commands like api, resources, etc.
func TestConventions_GroupCommandHasConfigFlags(t *testing.T) {
	cmd := assistant.Command()

	configFlag := cmd.PersistentFlags().Lookup("config")
	if configFlag == nil {
		t.Fatal("expected assistant group command to have persistent --config flag (via cmdconfig.Options.BindFlags)")
	}

	contextFlag := cmd.PersistentFlags().Lookup("context")
	if contextFlag == nil {
		t.Fatal("expected assistant group command to have persistent --context flag (via cmdconfig.Options.BindFlags)")
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

	// Set mutually exclusive flags — Validate should reject this.
	cmd.SetArgs([]string{"prompt", "test", "--context-id", "abc", "--continue"})

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
