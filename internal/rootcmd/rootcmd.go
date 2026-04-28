// Package rootcmd provides the shared command-building logic for the gcx root
// cobra command. Both the full CLI binary (cmd/gcx/root) and the embeddable
// library (gcxlib/internal/embed) call [New] to construct the command tree,
// avoiding duplication of flag definitions, PersistentPreRun, help setup, and
// subcommand registration.
//
// The dev subcommand is intentionally NOT registered here — the embed variant
// excludes it to avoid heavy transitive dependencies (Loki, OPA). Callers that
// need dev should pass it via the ExtraCommands option.
package rootcmd

import (
	"log/slog"
	"os"
	"path"
	"sync/atomic"

	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/grafana/gcx/cmd/gcx/api"
	assistantcmd "github.com/grafana/gcx/cmd/gcx/assistant"
	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/dashboards"
	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/cmd/gcx/helptree"
	logincmd "github.com/grafana/gcx/cmd/gcx/login"
	cmdproviders "github.com/grafana/gcx/cmd/gcx/providers"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/cmd/gcx/setup"
	skillscmd "github.com/grafana/gcx/cmd/gcx/skills"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// jsonFlagActive is set to true in PersistentPreRun when the resolved command
// has a --json flag that was explicitly changed by the user. This ensures
// handleError() in main.go emits JSON errors only for commands that actually
// support --json, avoiding false positives from naive os.Args pre-scanning.
//
//nolint:gochecknoglobals
var jsonFlagActive atomic.Bool

// IsJSONFlagActive reports whether the --json flag was actively set by the user
// on the command that was actually executed. Safe for concurrent use.
func IsJSONFlagActive() bool {
	return jsonFlagActive.Load()
}

// Options configures optional parts of the root command tree.
type Options struct {
	// ExtraCommands are added to the root command after the shared subcommands
	// but before the introspection commands (commands, help-tree) and agent
	// annotations. Use this to inject commands like dev.Command() that should
	// not be part of the shared set.
	ExtraCommands []*cobra.Command

	// Providers overrides the provider list. When nil, providers.All() is used.
	Providers []providers.Provider
}

// New builds the root cobra command with shared flags, PersistentPreRun,
// subcommands, and agent annotations. The caller can customise the tree
// via opts (nil is safe — defaults are applied).
func New(version string, opts *Options) *cobra.Command {
	if opts == nil {
		opts = &Options{}
	}

	pp := opts.Providers
	if pp == nil {
		pp = providers.All()
	}

	noColors := false
	noTruncate := false
	agentFlag := false
	verbosity := 0
	contextName := ""
	logHTTPPayload := false

	rootCmd := &cobra.Command{
		Use:           path.Base(os.Args[0]),
		Short:         "Control plane for Grafana Cloud operations",
		Long:          "gcx is a unified CLI for managing Grafana resources, dashboards, datasources, alerting, and Cloud product APIs (SLO, IRM, Synthetic Monitoring, Fleet, k6, and more).",
		SilenceUsage:  true,
		SilenceErrors: true, // We want to print errors ourselves
		Version:       version,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			// Track whether --json was explicitly set on the resolved command.
			// Only mark active when the command actually declares a --json flag,
			// preventing false positives for subcommands that don't support it.
			if f := cmd.Flags().Lookup("json"); f != nil && f.Changed {
				jsonFlagActive.Store(true)
			}

			// Detect TTY state first so all downstream decisions can use it.
			terminal.Detect()

			// Agent mode implies all pipe-aware behaviors regardless of actual TTY state.
			if agent.IsAgentMode() {
				terminal.SetPiped(true)
				terminal.SetNoTruncate(true)
				color.NoColor = true
				style.SetEnabled(false)
			}

			// Explicit --no-truncate flag overrides auto-detection.
			if noTruncate {
				terminal.SetNoTruncate(true)
			}

			// Explicit --no-color flag, NO_COLOR env var, or piped stdout disable color.
			if noColors || os.Getenv("NO_COLOR") != "" || terminal.IsPiped() {
				color.NoColor = true // globally disables colorized output
				style.SetEnabled(false)
			}

			logLevel := new(slog.LevelVar)
			logLevel.Set(slog.LevelError)
			// Multiplying the number of occurrences of the `-v` flag by 4 (gap between log levels in slog)
			// allows us to increase the logger's verbosity.
			logLevel.Set(logLevel.Level() - slog.Level(min(verbosity, 3)*4))

			logHandler := logs.NewHandler(os.Stderr, &logs.Options{
				Level: logLevel,
			})
			logger := logging.NewSLogLogger(logHandler)

			// Also set klog logger (used by k8s/client-go).
			klog.SetLoggerWithOptions(
				logr.FromSlogHandler(logHandler),
				klog.ContextualLogger(true),
			)

			ctx := logging.Context(cmd.Context(), logger)

			// Thread --context into Go context so provider config loaders
			// can discover it via config.ContextNameFromCtx().
			if contextName != "" {
				ctx = internalconfig.ContextWithName(ctx, contextName)
			}

			if logHTTPPayload {
				ctx = httputils.WithPayloadLogging(ctx, true)
			}

			cmd.SetContext(ctx)
		},
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "gcx",
		},
	}

	// Suppress unused variable lint — agentFlag is consumed by the flag
	// binding below but not read in Go code (agent mode is detected via env).
	_ = agentFlag

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(style.HelpFunc(defaultHelp))

	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetIn(os.Stdin)

	// --- Shared subcommands ---
	rootCmd.AddCommand(api.Command())
	rootCmd.AddCommand(assistantcmd.Command())
	rootCmd.AddCommand(logincmd.Command())
	rootCmd.AddCommand(config.Command())
	rootCmd.AddCommand(dashboards.Command())
	rootCmd.AddCommand(setup.Command())
	rootCmd.AddCommand(skillscmd.Command())
	rootCmd.AddCommand(datasources.Command())
	rootCmd.AddCommand(resources.Command())

	rootCmd.AddCommand(cmdproviders.Command(pp))
	for _, p := range pp {
		if p == nil {
			continue
		}
		rootCmd.AddCommand(p.Commands()...)
	}

	// --- Caller-supplied extra commands (e.g. dev.Command()) ---
	for _, cmd := range opts.ExtraCommands {
		rootCmd.AddCommand(cmd)
	}

	// Commands introspection — registered last so it sees the full command tree.
	// Pass configOpts so --validate can connect to a live Grafana instance.
	commandsConfigOpts := &config.Options{}
	commandsCmd := commands.Command(rootCmd, commandsConfigOpts)
	commandsConfigOpts.BindFlags(commandsCmd.Flags())
	rootCmd.AddCommand(commandsCmd)

	// Help-tree — compact text tree for agent context injection.
	// Also registered last to see the full command tree.
	rootCmd.AddCommand(helptree.Command(rootCmd))

	// Note: Provider adapter factories are registered via adapter.Register()
	// in each provider's init() function (same pattern as providers.Register).
	// The discovery.Registry picks them up via adapter.RegisterAll() when
	// resource commands create a registry instance.

	rootCmd.PersistentFlags().BoolVar(&noColors, "no-color", noColors, "Disable color output")
	rootCmd.PersistentFlags().BoolVar(&noTruncate, "no-truncate", false, "Disable table column truncation (auto-enabled when stdout is piped)")
	rootCmd.PersistentFlags().BoolVar(&agentFlag, "agent", false, "Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.")
	rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "Verbose mode. Multiple -v options increase the verbosity (maximum: 3).")
	rootCmd.PersistentFlags().StringVar(&contextName, "context", "", "Name of the context to use (overrides current-context in config)")
	rootCmd.PersistentFlags().BoolVar(&logHTTPPayload, "log-http-payload", false,
		"Log full HTTP request/response bodies (includes headers — may expose tokens)")

	// Initialize Cobra's built-in help/completion commands here so any code
	// traversing the command tree before ExecuteContext() sees the same shape
	// that Cobra will execute.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	// Apply centralized agent annotations (token_cost, llm_hint) to the
	// full command tree. Must run after all commands are registered.
	agent.ApplyAnnotations(rootCmd)
	return rootCmd
}
