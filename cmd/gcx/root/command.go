package root

import (
	"log/slog"
	"os"
	"path"
	"strings"
	"sync/atomic"

	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/grafana/gcx/cmd/gcx/api"
	assistantcmd "github.com/grafana/gcx/cmd/gcx/assistant"
	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/dashboards"
	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/cmd/gcx/dev"
	"github.com/grafana/gcx/cmd/gcx/helptree"
	logincmd "github.com/grafana/gcx/cmd/gcx/login"
	cmdproviders "github.com/grafana/gcx/cmd/gcx/providers"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/cmd/gcx/setup"
	skillscmd "github.com/grafana/gcx/cmd/gcx/skills"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	_ "github.com/grafana/gcx/internal/datasources/providers" // DatasourceProvider registrations — blank imports trigger init() self-registration.
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/notifier"
	"github.com/grafana/gcx/internal/providers"
	_ "github.com/grafana/gcx/internal/providers/aio11y"   // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/alert"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/appo11y"  // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/faro"     // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/fleet"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/irm"      // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/k6"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/kg"       // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/logs"     // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/metrics"  // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/profiles" // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/slo"      // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/synth"    // Provider registrations — blank imports trigger init() self-registration.
	_ "github.com/grafana/gcx/internal/providers/traces"   // Provider registrations — blank imports trigger init() self-registration.
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

func shouldNotifySkills(cmd *cobra.Command) bool {
	if os.Getenv(notifier.DisableNotifierEnvVar) != "" {
		return false
	}
	if agent.IsAgentMode() || IsJSONFlagActive() || terminal.IsPiped() {
		return false
	}
	if cmd == nil {
		return false
	}
	if isNonInteractiveCommand(cmd) {
		return false
	}
	if !hasInteractiveTextOutput(cmd) {
		return false
	}
	return true
}

func isNonInteractiveCommand(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "help", "completion", "__complete", "__completeNoDesc":
		return true
	}

	if flag := cmd.Flags().Lookup("help"); flag != nil && flag.Changed && flag.Value.String() == "true" {
		return true
	}
	if flag := cmd.Flags().Lookup("version"); flag != nil && flag.Changed && flag.Value.String() == "true" {
		return true
	}

	return false
}

func hasInteractiveTextOutput(cmd *cobra.Command) bool {
	flag := cmd.Flags().Lookup("output")
	if flag == nil {
		return true
	}

	switch strings.ToLower(flag.Value.String()) {
	case "text", "table", "wide", "graph", "pretty", "compact":
		return true
	default:
		return false
	}
}

// Command builds the root cobra command for the given version using the
// compile-time registered provider list.
func Command(version string) *cobra.Command {
	return newCommand(version, providers.All())
}

// newCommand builds the root cobra command with an explicit provider list.
// Callers that need to inject providers (e.g. tests) should use this directly.
// Nil entries in pp are silently skipped.
func newCommand(version string, pp []providers.Provider) *cobra.Command {
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
			jsonFlagActive.Store(false)
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
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			if !shouldNotifySkills(cmd) {
				return
			}
			_ = notifier.MaybeNotifySkills(cmd.ErrOrStderr())
		},
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "gcx",
		},
	}

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(style.HelpFunc(defaultHelp))

	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetIn(os.Stdin)

	rootCmd.AddCommand(api.Command())
	rootCmd.AddCommand(assistantcmd.Command())
	rootCmd.AddCommand(logincmd.Command())
	rootCmd.AddCommand(config.Command())
	rootCmd.AddCommand(dashboards.Command())
	rootCmd.AddCommand(dev.Command())
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
