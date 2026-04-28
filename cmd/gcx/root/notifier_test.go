package root //nolint:testpackage

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
)

func TestShouldNotifySkills_DefaultInteractiveTextCommand(t *testing.T) {
	resetNotifierTestState(t)

	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().StringP("output", "o", "text", "")

	if !shouldNotifySkills(cmd) {
		t.Fatal("shouldNotifySkills() = false, want true")
	}
}

func TestShouldNotifySkills_SuppressesNonInteractiveCases(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *cobra.Command)
	}{
		{
			name: "disabled by env var",
			setup: func(t *testing.T, _ *cobra.Command) {
				t.Helper()
				t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
			},
		},
		{
			name: "agent mode",
			setup: func(t *testing.T, _ *cobra.Command) {
				t.Helper()
				t.Setenv("GCX_AGENT_MODE", "1")
				agent.ResetForTesting()
			},
		},
		{
			name: "json flag active",
			setup: func(_ *testing.T, _ *cobra.Command) {
				jsonFlagActive.Store(true)
			},
		},
		{
			name: "stdout piped",
			setup: func(_ *testing.T, _ *cobra.Command) {
				terminal.SetPiped(true)
			},
		},
		{
			name: "help flag",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Flags().Bool("help", false, "")
				_ = cmd.Flags().Set("help", "true")
			},
		},
		{
			name: "version flag",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Flags().Bool("version", false, "")
				_ = cmd.Flags().Set("version", "true")
			},
		},
		{
			name: "help command",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Use = "help"
			},
		},
		{
			name: "completion command",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Use = "completion"
			},
		},
		{
			name: "cobra hidden completion helper",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Use = "__complete"
			},
		},
		{
			name: "resolved json output",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Flags().StringP("output", "o", "json", "")
			},
		},
		{
			name: "resolved yaml output",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Flags().StringP("output", "o", "text", "")
				_ = cmd.Flags().Set("output", "yaml")
			},
		},
		{
			name: "resolved raw output",
			setup: func(_ *testing.T, cmd *cobra.Command) {
				cmd.Flags().StringP("output", "o", "raw", "")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetNotifierTestState(t)

			cmd := &cobra.Command{Use: "list"}
			tc.setup(t, cmd)
			if cmd.Flags().Lookup("output") == nil {
				cmd.Flags().StringP("output", "o", "text", "")
			}

			if shouldNotifySkills(cmd) {
				t.Fatal("shouldNotifySkills() = true, want false")
			}
		})
	}
}

func TestHasInteractiveTextOutput_AllowsKnownTextFormats(t *testing.T) {
	tests := []string{"text", "table", "wide", "graph", "pretty", "compact"}
	for _, format := range tests {
		t.Run(format, func(t *testing.T) {
			cmd := &cobra.Command{Use: "list"}
			cmd.Flags().StringP("output", "o", format, "")

			if !hasInteractiveTextOutput(cmd) {
				t.Fatalf("hasInteractiveTextOutput() = false for %q, want true", format)
			}
		})
	}
}

func resetNotifierTestState(t *testing.T) {
	t.Helper()

	for _, env := range []string{
		"GCX_NO_UPDATE_NOTIFIER",
		"GCX_AGENT_MODE",
		"CLAUDECODE",
		"CLAUDE_CODE",
		"CURSOR_AGENT",
		"GITHUB_COPILOT",
		"AMAZON_Q",
		"OPENCODE",
	} {
		t.Setenv(env, "")
	}

	agent.ResetForTesting()
	terminal.ResetForTesting()
	jsonFlagActive.Store(false)
}
